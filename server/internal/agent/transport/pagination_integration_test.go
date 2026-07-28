package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresAgentPaginationAndFocusedThread(t *testing.T) {
	database := newAgentTestDatabase(t)
	_, service := newAgentDataServices(t, database.pool)
	actorA := requestcontext.Actor{
		UserID:    agentTestUserA,
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	actorB := requestcontext.Actor{
		UserID:    agentTestUserB,
		SessionID: "20000000-0000-4000-8000-000000000002",
	}

	const threadCount = 105
	threads := make([]Thread, 0, threadCount)
	for index := 0; index < threadCount; index++ {
		thread, err := service.CreateThread(context.Background(), actorA, "")
		if err != nil {
			t.Fatalf("create Thread %d: %v", index, err)
		}
		threads = append(threads, thread)
	}
	foreignThread, err := service.CreateThread(context.Background(), actorB, "")
	if err != nil {
		t.Fatalf("create foreign Thread: %v", err)
	}

	if _, found, err := service.GetFocusedThread(
		context.Background(),
		actorA,
	); err != nil || found {
		t.Fatalf("initial focused Thread = found %t, error %v; want none", found, err)
	}
	focused, err := service.SetFocusedThread(
		context.Background(),
		actorA,
		threads[57].ID,
	)
	if err != nil {
		t.Fatalf("set focused Thread: %v", err)
	}
	if focused.ID != threads[57].ID {
		t.Fatalf("focused Thread ID = %q, want %q", focused.ID, threads[57].ID)
	}
	replayed, err := service.SetFocusedThread(
		context.Background(),
		actorA,
		threads[57].ID,
	)
	if err != nil || replayed.ID != focused.ID {
		t.Fatalf("replay focused Thread = %#v, %v", replayed, err)
	}
	if _, err := service.SetFocusedThread(
		context.Background(),
		actorA,
		foreignThread.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner focused Thread error = %v, want not found", err)
	}
	if _, err := database.pool.Exec(context.Background(), `
WITH boundary AS (
    SELECT MAX(created_at) AS updated_at
    FROM agent_threads
    WHERE owner_user_id = $1
)
UPDATE agent_threads
SET updated_at = boundary.updated_at
FROM boundary
WHERE owner_user_id = $1`,
		actorA.UserID,
	); err != nil {
		t.Fatalf("align Thread timestamps for tuple keyset test: %v", err)
	}

	expected, err := service.ListThreads(context.Background(), actorA)
	if err != nil {
		t.Fatalf("list expected Threads: %v", err)
	}
	var (
		cursor    string
		collected []Thread
	)
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > threadCount {
			t.Fatal("Thread pagination did not terminate")
		}
		page, err := service.PageThreads(
			context.Background(),
			actorA,
			17,
			cursor,
		)
		if err != nil {
			t.Fatalf("page Threads %d: %v", pageNumber, err)
		}
		if page.FocusedThreadID != focused.ID {
			t.Fatalf(
				"page %d focused Thread = %q, want %q",
				pageNumber,
				page.FocusedThreadID,
				focused.ID,
			)
		}
		if len(page.Threads) > 17 {
			t.Fatalf("page %d size = %d, want <= 17", pageNumber, len(page.Threads))
		}
		collected = append(collected, page.Threads...)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(collected) != len(expected) {
		t.Fatalf("collected Thread count = %d, want %d", len(collected), len(expected))
	}
	seenThreads := make(map[string]struct{}, len(collected))
	for index, thread := range collected {
		if thread.ID != expected[index].ID {
			t.Fatalf(
				"Thread %d ID = %q, want %q",
				index,
				thread.ID,
				expected[index].ID,
			)
		}
		if _, duplicate := seenThreads[thread.ID]; duplicate {
			t.Fatalf("duplicate Thread %q", thread.ID)
		}
		seenThreads[thread.ID] = struct{}{}
	}

	firstPage, err := service.PageThreads(context.Background(), actorA, 10, "")
	if err != nil || firstPage.NextCursor == "" {
		t.Fatalf("first Thread page = %#v, %v; want cursor", firstPage, err)
	}
	foreignPage, err := service.PageThreads(
		context.Background(),
		actorB,
		10,
		firstPage.NextCursor,
	)
	if err != nil {
		t.Fatalf("use keyset boundary under another owner: %v", err)
	}
	for _, thread := range foreignPage.Threads {
		if thread.OwnerID != actorB.UserID {
			t.Fatalf("cursor crossed owner boundary: %#v", thread)
		}
	}

	const messageCount = 1001
	if _, err := database.pool.Exec(context.Background(), `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    created_at
)
SELECT
    (
        '30000000-0000-4000-8000-' ||
        LPAD(TO_HEX(generated.sequence_no), 12, '0')
    )::uuid,
    $1,
    $2,
    generated.sequence_no,
    'user',
    'bulk-' || generated.sequence_no,
    'message ' || generated.sequence_no,
    CURRENT_TIMESTAMP
FROM GENERATE_SERIES(1, $3) AS generated(sequence_no);
`,
		actorA.UserID,
		threads[0].ID,
		messageCount,
	); err != nil {
		t.Fatalf("seed Messages: %v", err)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE agent_threads
SET
    next_message_sequence = $3 + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $2 AND owner_user_id = $1`,
		actorA.UserID,
		threads[0].ID,
		messageCount,
	); err != nil {
		t.Fatalf("advance seeded Message sequence: %v", err)
	}

	seenMessages := make(map[int64]struct{}, messageCount)
	cursor = ""
	var previousPageOldest int64
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > messageCount {
			t.Fatal("Message pagination did not terminate")
		}
		page, err := service.PageMessages(
			context.Background(),
			actorA,
			threads[0].ID,
			73,
			cursor,
		)
		if err != nil {
			t.Fatalf("page Messages %d: %v", pageNumber, err)
		}
		if len(page.Messages) == 0 {
			t.Fatalf("page %d unexpectedly empty before termination", pageNumber)
		}
		if len(page.Messages) > 73 {
			t.Fatalf("page %d size = %d, want <= 73", pageNumber, len(page.Messages))
		}
		for index, message := range page.Messages {
			if index > 0 &&
				page.Messages[index-1].Sequence >= message.Sequence {
				t.Fatalf("page %d is not sequence ASC: %#v", pageNumber, page.Messages)
			}
			if _, duplicate := seenMessages[message.Sequence]; duplicate {
				t.Fatalf("duplicate Message sequence %d", message.Sequence)
			}
			seenMessages[message.Sequence] = struct{}{}
		}
		if pageNumber == 0 {
			gotNewest := page.Messages[len(page.Messages)-1].Sequence
			if gotNewest != messageCount {
				t.Fatalf("first page newest sequence = %d, want %d", gotNewest, messageCount)
			}
		} else {
			gotNewest := page.Messages[len(page.Messages)-1].Sequence
			if gotNewest >= previousPageOldest {
				t.Fatalf(
					"page %d newest sequence = %d, previous oldest = %d",
					pageNumber,
					gotNewest,
					previousPageOldest,
				)
			}
		}
		previousPageOldest = page.Messages[0].Sequence
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seenMessages) != messageCount {
		t.Fatalf("collected Message count = %d, want %d", len(seenMessages), messageCount)
	}
	for sequence := int64(1); sequence <= messageCount; sequence++ {
		if _, found := seenMessages[sequence]; !found {
			t.Fatalf("missing Message sequence %d", sequence)
		}
	}

	messageCursor, err := encodeMessagePageCursor(MessagePageCursor{
		ThreadID:       threads[0].ID,
		BeforeSequence: 500,
	})
	if err != nil {
		t.Fatalf("encode Message cursor: %v", err)
	}
	if _, err := service.PageMessages(
		context.Background(),
		actorA,
		threads[1].ID,
		73,
		messageCursor,
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-Thread Message cursor error = %v, want invalid", err)
	}
	if _, err := service.PageMessages(
		context.Background(),
		actorB,
		threads[0].ID,
		73,
		messageCursor,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Message page error = %v, want not found", err)
	}

	const focusWriters = 24
	start := make(chan struct{})
	failures := make(chan error, focusWriters)
	var writers sync.WaitGroup
	for index := 0; index < focusWriters; index++ {
		threadID := threads[index].ID
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			_, setErr := service.SetFocusedThread(
				context.Background(),
				actorA,
				threadID,
			)
			if setErr != nil {
				failures <- setErr
			}
		}()
	}
	close(start)
	writers.Wait()
	close(failures)
	for setErr := range failures {
		t.Errorf("concurrent focused Thread selection: %v", setErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	var focusRows int
	if err := database.pool.QueryRow(context.Background(), `
SELECT COUNT(*)
FROM agent_thread_focuses
WHERE owner_user_id = $1`,
		actorA.UserID,
	).Scan(&focusRows); err != nil {
		t.Fatalf("count focused rows: %v", err)
	}
	if focusRows != 1 {
		t.Fatalf("focused row count = %d, want 1", focusRows)
	}
	concurrentFocus, found, err := service.GetFocusedThread(
		context.Background(),
		actorA,
	)
	if err != nil || !found {
		t.Fatalf("get concurrent focused Thread = %#v, %t, %v", concurrentFocus, found, err)
	}
	if _, allowed := seenThreads[concurrentFocus.ID]; !allowed {
		t.Fatalf("unexpected concurrently focused Thread %q", concurrentFocus.ID)
	}

	assertFocusedThreadOwnerConstraint(
		t,
		database,
		actorB.UserID,
		threads[0].ID,
	)

	reopened := database.reopen(t)
	_, recoveredService := newAgentDataServices(t, reopened)
	recoveredFocus, found, err := recoveredService.GetFocusedThread(
		context.Background(),
		actorA,
	)
	if err != nil || !found || recoveredFocus.ID != concurrentFocus.ID {
		t.Fatalf(
			"recovered focus = %#v, found %t, error %v; want %q",
			recoveredFocus,
			found,
			err,
			concurrentFocus.ID,
		)
	}
	if err := recoveredService.ClearFocusedThread(
		context.Background(),
		actorA,
	); err != nil {
		t.Fatalf("clear focused Thread: %v", err)
	}
	if err := recoveredService.ClearFocusedThread(
		context.Background(),
		actorA,
	); err != nil {
		t.Fatalf("repeat clear focused Thread: %v", err)
	}
	if _, found, err := recoveredService.GetFocusedThread(
		context.Background(),
		actorA,
	); err != nil || found {
		t.Fatalf("focused Thread after clear = found %t, error %v", found, err)
	}
	if _, err := recoveredService.SetFocusedThread(
		context.Background(),
		actorA,
		threads[2].ID,
	); err != nil {
		t.Fatalf("set focused Thread before cascade: %v", err)
	}
	if _, err := reopened.Exec(context.Background(), `
DELETE FROM agent_threads
WHERE id = $1 AND owner_user_id = $2`,
		threads[2].ID,
		actorA.UserID,
	); err != nil {
		t.Fatalf("delete focused Thread: %v", err)
	}
	if _, found, err := recoveredService.GetFocusedThread(
		context.Background(),
		actorA,
	); err != nil || found {
		t.Fatalf("focused Thread after cascade = found %t, error %v", found, err)
	}
}

func TestPostgresAgentPaginationAndFocusedHTTP(t *testing.T) {
	database := newAgentTestDatabase(t)
	matterService, service := newAgentDataServices(t, database.pool)
	actorA := requestcontext.Actor{
		UserID:    agentTestUserA,
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	actorB := requestcontext.Actor{
		UserID:    agentTestUserB,
		SessionID: "20000000-0000-4000-8000-000000000002",
	}
	handler, err := NewHTTPHandler(
		service,
		matterService,
		authenticatorFunc(func(
			_ context.Context,
			token string,
		) (requestcontext.Actor, error) {
			switch token {
			case "token-a":
				return actorA, nil
			case "token-b":
				return actorB, nil
			default:
				return requestcontext.Actor{}, identity.ErrAuthenticationRequired
			}
		}),
		func() string { return "corr_agent_pagination_test" },
	)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterRoutes(router)

	noFocus := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/focused",
		"",
		"token-a",
	)
	if noFocus.Code != http.StatusNoContent || noFocus.Body.Len() != 0 {
		t.Fatalf("empty focused response: %d %q", noFocus.Code, noFocus.Body.String())
	}

	threads := make([]Thread, 0, defaultThreadPageSize+1)
	for index := 0; index < defaultThreadPageSize+1; index++ {
		thread, createErr := service.CreateThread(
			context.Background(),
			actorA,
			"",
		)
		if createErr != nil {
			t.Fatalf("create Thread %d: %v", index, createErr)
		}
		threads = append(threads, thread)
	}
	foreignThread, err := service.CreateThread(context.Background(), actorB, "")
	if err != nil {
		t.Fatalf("create foreign Thread: %v", err)
	}

	setFocus := performAgentRequest(
		router,
		http.MethodPut,
		"/v1/agent-threads/focused",
		`{"thread_id":"`+threads[0].ID+`"}`,
		"token-a",
	)
	if setFocus.Code != http.StatusOK {
		t.Fatalf("set focused response: %d %s", setFocus.Code, setFocus.Body)
	}
	crossOwnerFocus := performAgentRequest(
		router,
		http.MethodPut,
		"/v1/agent-threads/focused",
		`{"thread_id":"`+foreignThread.ID+`"}`,
		"token-a",
	)
	if crossOwnerFocus.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-owner focused response: %d %s",
			crossOwnerFocus.Code,
			crossOwnerFocus.Body,
		)
	}

	threadList := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads",
		"",
		"token-a",
	)
	if threadList.Code != http.StatusOK {
		t.Fatalf("default Thread page response: %d %s", threadList.Code, threadList.Body)
	}
	var threadPage struct {
		Threads []struct {
			ID string `json:"thread_id"`
		} `json:"threads"`
		FocusedThreadID string `json:"focused_thread_id"`
		NextCursor      string `json:"next_cursor"`
	}
	if err := json.Unmarshal(threadList.Body.Bytes(), &threadPage); err != nil {
		t.Fatalf("decode Thread page: %v", err)
	}
	if len(threadPage.Threads) != defaultThreadPageSize ||
		threadPage.FocusedThreadID != threads[0].ID ||
		threadPage.NextCursor == "" {
		t.Fatalf("default Thread page = %#v", threadPage)
	}

	for _, path := range []string{
		"/v1/agent-threads?page_size=0",
		"/v1/agent-threads?page_size=101",
		"/v1/agent-threads?page_size=20&page_size=21",
		"/v1/agent-threads?offset=20",
		"/v1/agent-threads?cursor=invalid",
	} {
		response := performAgentRequest(
			router,
			http.MethodGet,
			path,
			"",
			"token-a",
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid Thread page %q: %d %s", path, response.Code, response.Body)
		}
	}

	const httpMessageCount = defaultMessagePageSize + 1
	if _, err := database.pool.Exec(context.Background(), `
INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content,
    created_at
)
SELECT
    (
        '40000000-0000-4000-8000-' ||
        LPAD(TO_HEX(generated.sequence_no), 12, '0')
    )::uuid,
    $1,
    $2,
    generated.sequence_no,
    'user',
    'http-' || generated.sequence_no,
    'message ' || generated.sequence_no,
    CURRENT_TIMESTAMP
FROM GENERATE_SERIES(1, $3) AS generated(sequence_no);
`,
		actorA.UserID,
		threads[0].ID,
		httpMessageCount,
	); err != nil {
		t.Fatalf("seed HTTP Messages: %v", err)
	}
	if _, err := database.pool.Exec(context.Background(), `
UPDATE agent_threads
SET next_message_sequence = $3 + 1
WHERE id = $2 AND owner_user_id = $1`,
		actorA.UserID,
		threads[0].ID,
		httpMessageCount,
	); err != nil {
		t.Fatalf("advance seeded HTTP Message sequence: %v", err)
	}
	messageList := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threads[0].ID+"/messages",
		"",
		"token-a",
	)
	if messageList.Code != http.StatusOK {
		t.Fatalf("default Message page response: %d %s", messageList.Code, messageList.Body)
	}
	var messagePage struct {
		Messages []struct {
			Sequence int64 `json:"sequence"`
		} `json:"messages"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(messageList.Body.Bytes(), &messagePage); err != nil {
		t.Fatalf("decode Message page: %v", err)
	}
	if len(messagePage.Messages) != defaultMessagePageSize ||
		messagePage.Messages[0].Sequence != 2 ||
		messagePage.Messages[len(messagePage.Messages)-1].Sequence !=
			httpMessageCount ||
		messagePage.NextCursor == "" {
		t.Fatalf("default Message page = %#v", messagePage)
	}
	olderMessages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threads[0].ID+"/messages?cursor="+
			messagePage.NextCursor,
		"",
		"token-a",
	)
	if olderMessages.Code != http.StatusOK {
		t.Fatalf("older Message page response: %d %s", olderMessages.Code, olderMessages.Body)
	}
	var olderPage struct {
		Messages []struct {
			Sequence int64 `json:"sequence"`
		} `json:"messages"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(olderMessages.Body.Bytes(), &olderPage); err != nil {
		t.Fatalf("decode older Message page: %v", err)
	}
	if len(olderPage.Messages) != 1 ||
		olderPage.Messages[0].Sequence != 1 ||
		olderPage.NextCursor != "" {
		t.Fatalf("older Message page = %#v", olderPage)
	}

	wrongEndpointCursor := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threads[0].ID+"/messages?cursor="+
			threadPage.NextCursor,
		"",
		"token-a",
	)
	if wrongEndpointCursor.Code != http.StatusBadRequest {
		t.Fatalf(
			"wrong-endpoint cursor response: %d %s",
			wrongEndpointCursor.Code,
			wrongEndpointCursor.Body,
		)
	}
	wrongThreadCursor := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threads[1].ID+"/messages?cursor="+
			messagePage.NextCursor,
		"",
		"token-a",
	)
	if wrongThreadCursor.Code != http.StatusBadRequest {
		t.Fatalf(
			"wrong-Thread cursor response: %d %s",
			wrongThreadCursor.Code,
			wrongThreadCursor.Body,
		)
	}
	crossOwnerMessages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threads[0].ID+"/messages?cursor="+
			messagePage.NextCursor,
		"",
		"token-b",
	)
	if crossOwnerMessages.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-owner Message cursor response: %d %s",
			crossOwnerMessages.Code,
			crossOwnerMessages.Body,
		)
	}

	clearFocus := performAgentRequest(
		router,
		http.MethodDelete,
		"/v1/agent-threads/focused",
		"",
		"token-a",
	)
	if clearFocus.Code != http.StatusNoContent || clearFocus.Body.Len() != 0 {
		t.Fatalf("clear focused response: %d %q", clearFocus.Code, clearFocus.Body.String())
	}
	repeatedClear := performAgentRequest(
		router,
		http.MethodDelete,
		"/v1/agent-threads/focused",
		"",
		"token-a",
	)
	if repeatedClear.Code != http.StatusNoContent {
		t.Fatalf("repeat clear focused response: %d %s", repeatedClear.Code, repeatedClear.Body)
	}
}

func assertFocusedThreadOwnerConstraint(
	t *testing.T,
	database agentTestDatabase,
	ownerID string,
	foreignThreadID string,
) {
	t.Helper()
	_, err := database.pool.Exec(context.Background(), `
INSERT INTO agent_thread_focuses (
    owner_user_id,
    thread_id
) VALUES ($1, $2)`,
		ownerID,
		foreignThreadID,
	)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("cross-owner focus error = %v, want PostgreSQL constraint", err)
	}
	if postgresError.Code != "23503" ||
		postgresError.ConstraintName !=
			"agent_thread_focuses_thread_owner_fkey" {
		t.Fatalf(
			"cross-owner focus PostgreSQL error = %s/%s, want 23503/%s",
			postgresError.Code,
			postgresError.ConstraintName,
			"agent_thread_focuses_thread_owner_fkey",
		)
	}
}
