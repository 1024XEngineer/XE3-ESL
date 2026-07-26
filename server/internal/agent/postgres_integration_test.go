package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/matter"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/migration"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	agentTestUserA = "10000000-0000-4000-8000-000000000001"
	agentTestUserB = "10000000-0000-4000-8000-000000000002"
)

func TestPostgresAgentDataVerticalSlice(t *testing.T) {
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

	matterA, err := matterService.Create(
		context.Background(),
		actorA,
		"Customer renewal meeting",
	)
	if err != nil {
		t.Fatalf("create matter A: %v", err)
	}
	secondMatterA, err := matterService.Create(
		context.Background(),
		actorA,
		"Quarterly presentation",
	)
	if err != nil {
		t.Fatalf("create second matter A: %v", err)
	}
	matterB, err := matterService.Create(
		context.Background(),
		actorB,
		"Private interview",
	)
	if err != nil {
		t.Fatalf("create matter B: %v", err)
	}
	listA, err := matterService.List(context.Background(), actorA)
	if err != nil {
		t.Fatalf("list matters A: %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("matter A count = %d, want 2", len(listA))
	}
	if _, err := matterService.ReadOwned(
		context.Background(),
		actorA,
		matterB.ID,
	); !errors.Is(err, matter.ErrNotFound) {
		t.Fatalf("cross-owner Matter read error = %v, want not found", err)
	}

	threadA, err := service.CreateThread(
		context.Background(),
		actorA,
		matterA.ID,
	)
	if err != nil {
		t.Fatalf("create thread A: %v", err)
	}
	if threadA.ActiveMatterID != matterA.ID {
		t.Fatalf("active matter = %q, want %q", threadA.ActiveMatterID, matterA.ID)
	}
	threadB, err := service.CreateThread(
		context.Background(),
		actorB,
		matterB.ID,
	)
	if err != nil {
		t.Fatalf("create thread B: %v", err)
	}
	if _, err := service.CreateThread(
		context.Background(),
		actorA,
		matterB.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Matter link error = %v, want not found", err)
	}
	if _, err := service.GetThread(
		context.Background(),
		actorA,
		threadB.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Thread read error = %v, want not found", err)
	}
	if _, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		threadB.ID,
		"cross-owner-message",
		"must not be stored",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Message write error = %v, want not found", err)
	}
	if _, err := service.ListMessages(
		context.Background(),
		actorA,
		threadB.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Message read error = %v, want not found", err)
	}

	first, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		threadA.ID,
		"client-message-1",
		"Help me prepare a concise opening.",
	)
	if err != nil {
		t.Fatalf("append first message: %v", err)
	}
	replayed, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		threadA.ID,
		"client-message-1",
		"Help me prepare a concise opening.",
	)
	if err != nil {
		t.Fatalf("replay first message: %v", err)
	}
	if replayed.ID != first.ID || replayed.Sequence != first.Sequence {
		t.Fatalf("replay = %#v, want original %#v", replayed, first)
	}
	if _, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		threadA.ID,
		"client-message-1",
		"Different content must conflict.",
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v, want idempotency conflict", err)
	}

	const concurrentMessages = 16
	start := make(chan struct{})
	results := make(chan Message, concurrentMessages)
	failures := make(chan error, concurrentMessages)
	var writers sync.WaitGroup
	for index := 0; index < concurrentMessages; index++ {
		index := index
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			message, err := service.AppendUserMessage(
				context.Background(),
				actorA,
				threadA.ID,
				fmt.Sprintf("parallel-%02d", index),
				fmt.Sprintf("parallel content %02d", index),
			)
			if err != nil {
				failures <- err
				return
			}
			results <- message
		}()
	}
	close(start)
	writers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Errorf("parallel append: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	sequences := []int64{first.Sequence}
	for message := range results {
		sequences = append(sequences, message.Sequence)
	}
	sort.Slice(sequences, func(left, right int) bool {
		return sequences[left] < sequences[right]
	})
	for index, sequence := range sequences {
		want := int64(index + 1)
		if sequence != want {
			t.Fatalf("sequence[%d] = %d, want %d", index, sequence, want)
		}
	}

	sameKeyThread, err := service.CreateThread(
		context.Background(),
		actorA,
		"",
	)
	if err != nil {
		t.Fatalf("create same-key thread: %v", err)
	}
	const sameKeyWriters = 8
	sameKeyStart := make(chan struct{})
	sameKeyResults := make(chan Message, sameKeyWriters)
	sameKeyFailures := make(chan error, sameKeyWriters)
	writers = sync.WaitGroup{}
	for range sameKeyWriters {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-sameKeyStart
			message, err := service.AppendUserMessage(
				context.Background(),
				actorA,
				sameKeyThread.ID,
				"same-client-message",
				"Store this exactly once.",
			)
			if err != nil {
				sameKeyFailures <- err
				return
			}
			sameKeyResults <- message
		}()
	}
	close(sameKeyStart)
	writers.Wait()
	close(sameKeyResults)
	close(sameKeyFailures)
	for err := range sameKeyFailures {
		t.Errorf("same-key append: %v", err)
	}
	var sameKeyID string
	for result := range sameKeyResults {
		if sameKeyID == "" {
			sameKeyID = result.ID
		}
		if result.ID != sameKeyID || result.Sequence != 1 {
			t.Fatalf("same-key result = %#v, want one sequence-1 message", result)
		}
	}

	panicThread, err := service.CreateThread(
		context.Background(),
		actorA,
		"",
	)
	if err != nil {
		t.Fatalf("create panic rollback thread: %v", err)
	}
	panicRepository, err := NewPostgresRepository(
		database.pool,
		idGeneratorFunc(func() (string, error) {
			panic("test ID generator panic")
		}),
	)
	if err != nil {
		t.Fatalf("new panic repository: %v", err)
	}
	acquiredBeforePanic := database.pool.Stat().AcquiredConns()
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = panicRepository.AppendUserMessage(
			context.Background(),
			actorA.UserID,
			panicThread.ID,
			"panic-rolls-back",
			"this transaction must be released",
		)
	}()
	if recovered == nil {
		t.Fatal("panic ID generator did not panic")
	}
	if acquiredAfterPanic := database.pool.Stat().AcquiredConns(); acquiredAfterPanic !=
		acquiredBeforePanic {
		t.Fatalf(
			"acquired connections after panic = %d, want %d",
			acquiredAfterPanic,
			acquiredBeforePanic,
		)
	}
	if _, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		panicThread.ID,
		"after-panic",
		"the Thread lock was released",
	); err != nil {
		t.Fatalf("append after repository panic: %v", err)
	}

	link, err := service.SetActiveMatter(
		context.Background(),
		actorA,
		threadA.ID,
		secondMatterA.ID,
	)
	if err != nil {
		t.Fatalf("change active matter: %v", err)
	}
	if !link.Active || link.MatterID != secondMatterA.ID {
		t.Fatalf("unexpected active link: %#v", link)
	}
	threadAfterSelection, err := service.GetThread(
		context.Background(),
		actorA,
		threadA.ID,
	)
	if err != nil {
		t.Fatalf("get Thread after active Matter selection: %v", err)
	}
	replayedLink, err := service.SetActiveMatter(
		context.Background(),
		actorA,
		threadA.ID,
		secondMatterA.ID,
	)
	if err != nil {
		t.Fatalf("replay active Matter selection: %v", err)
	}
	threadAfterSelectionReplay, err := service.GetThread(
		context.Background(),
		actorA,
		threadA.ID,
	)
	if err != nil {
		t.Fatalf("get Thread after active Matter replay: %v", err)
	}
	if !replayedLink.LinkedAt.Equal(link.LinkedAt) ||
		!replayedLink.UpdatedAt.Equal(link.UpdatedAt) ||
		!threadAfterSelectionReplay.UpdatedAt.Equal(threadAfterSelection.UpdatedAt) {
		t.Fatalf(
			"replayed active Matter changed timestamps: %#v / %#v",
			link,
			replayedLink,
		)
	}
	if _, err := service.SetActiveMatter(
		context.Background(),
		actorA,
		threadA.ID,
		matterB.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner active Matter error = %v, want not found", err)
	}

	archived, err := matterService.ChangeStatus(
		context.Background(),
		actorA,
		matterA.ID,
		matterA.Version,
		matter.StatusArchived,
	)
	if err != nil {
		t.Fatalf("archive Matter: %v", err)
	}
	if _, err := service.SetActiveMatter(
		context.Background(),
		actorA,
		threadA.ID,
		archived.ID,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("select archived Matter error = %v, want conflict", err)
	}
	reopened, err := matterService.ChangeStatus(
		context.Background(),
		actorA,
		archived.ID,
		archived.Version,
		matter.StatusActive,
	)
	if err != nil || reopened.Status != matter.StatusActive {
		t.Fatalf("reopen Matter = %#v, %v", reopened, err)
	}

	assertCrossOwnerDatabaseConstraints(
		t,
		database.pool,
		threadA.ID,
		threadB.ID,
		matterA.ID,
		matterB.ID,
	)
	assertRestrictedCrossModuleDeletes(
		t,
		database.pool,
		secondMatterA.ID,
	)

	messages, err := service.ListMessages(
		context.Background(),
		actorA,
		threadA.ID,
	)
	if err != nil {
		t.Fatalf("list messages before reconnect: %v", err)
	}
	if len(messages) != concurrentMessages+1 {
		t.Fatalf(
			"message count = %d, want %d",
			len(messages),
			concurrentMessages+1,
		)
	}
	database.pool.Close()
	reopenedPool := database.reopen(t)
	recoveredMatterService, recoveredService := newAgentDataServices(t, reopenedPool)
	recoveredMatter, err := recoveredMatterService.ReadOwned(
		context.Background(),
		actorA,
		matterA.ID,
	)
	if err != nil ||
		recoveredMatter.Title != reopened.Title ||
		recoveredMatter.Status != reopened.Status ||
		recoveredMatter.Version != reopened.Version {
		t.Fatalf("recovered Matter = %#v, %v", recoveredMatter, err)
	}
	recoveredThread, err := recoveredService.GetThread(
		context.Background(),
		actorA,
		threadA.ID,
	)
	if err != nil || recoveredThread.ActiveMatterID != secondMatterA.ID {
		t.Fatalf("recovered thread = %#v, %v", recoveredThread, err)
	}
	recoveredMessages, err := recoveredService.ListMessages(
		context.Background(),
		actorA,
		threadA.ID,
	)
	if err != nil || len(recoveredMessages) != len(messages) {
		t.Fatalf(
			"recovered messages = %d, %v; want %d",
			len(recoveredMessages),
			err,
			len(messages),
		)
	}
}

func TestPostgresActiveMatterBindingSerializesWithLifecycleTransition(
	t *testing.T,
) {
	testCases := []struct {
		name         string
		targetStatus matter.Status
		createThread bool
	}{
		{
			name:         "create thread while Matter is archived",
			targetStatus: matter.StatusArchived,
			createThread: true,
		},
		{
			name:         "select Matter while it is completed",
			targetStatus: matter.StatusCompleted,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := newAgentTestDatabase(t)
			matterService, normalService := newAgentDataServices(t, database.pool)
			actor := requestcontext.Actor{
				UserID:    agentTestUserA,
				SessionID: "20000000-0000-4000-8000-000000000001",
			}
			item, err := matterService.Create(
				context.Background(),
				actor,
				"Concurrent lifecycle transition",
			)
			if err != nil {
				t.Fatalf("create Matter: %v", err)
			}
			var thread Thread
			if !testCase.createThread {
				thread, err = normalService.CreateThread(
					context.Background(),
					actor,
					"",
				)
				if err != nil {
					t.Fatalf("create Thread: %v", err)
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			transition, err := database.pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin Matter transition: %v", err)
			}
			defer func() {
				_ = transition.Rollback(context.Background())
			}()
			if _, err := transition.Exec(ctx, `
UPDATE matters
SET
    status = $3,
    version = version + 1,
    updated_at = GREATEST(
        CURRENT_TIMESTAMP,
        updated_at + INTERVAL '1 microsecond'
    )
WHERE id = $1 AND owner_user_id = $2`,
				item.ID,
				actor.UserID,
				string(testCase.targetStatus),
			); err != nil {
				t.Fatalf("stage Matter transition: %v", err)
			}

			lockAttempted := make(chan struct{}, 1)
			observedDatabase := &queryObservingPostgreSQL{
				Pool: database.pool,
				observeQuery: func(query string) {
					if strings.Contains(query, "FROM matters") &&
						strings.Contains(query, "FOR UPDATE") {
						select {
						case lockAttempted <- struct{}{}:
						default:
						}
					}
				},
			}
			repository, err := NewPostgresRepository(
				observedDatabase,
				identity.NewUUIDv4Generator(nil),
			)
			if err != nil {
				t.Fatalf("new observed Agent repository: %v", err)
			}
			observedService, err := NewService(repository, matterService)
			if err != nil {
				t.Fatalf("new observed Agent service: %v", err)
			}
			result := make(chan error, 1)
			go func() {
				if testCase.createThread {
					_, operationErr := observedService.CreateThread(
						ctx,
						actor,
						item.ID,
					)
					result <- operationErr
					return
				}
				_, operationErr := observedService.SetActiveMatter(
					ctx,
					actor,
					thread.ID,
					item.ID,
				)
				result <- operationErr
			}()

			select {
			case <-lockAttempted:
			case operationErr := <-result:
				t.Fatalf(
					"binding completed before atomic Matter lock: %v",
					operationErr,
				)
			case <-ctx.Done():
				t.Fatal("binding did not attempt the atomic Matter lock")
			}
			select {
			case operationErr := <-result:
				t.Fatalf(
					"binding escaped the uncommitted Matter transition: %v",
					operationErr,
				)
			default:
			}
			if err := transition.Commit(ctx); err != nil {
				t.Fatalf("commit Matter transition: %v", err)
			}
			select {
			case operationErr := <-result:
				if !errors.Is(operationErr, ErrConflict) {
					t.Fatalf(
						"binding error after Matter transition = %v, want conflict",
						operationErr,
					)
				}
			case <-ctx.Done():
				t.Fatal("binding did not finish after Matter transition")
			}

			if testCase.createThread {
				threads, err := normalService.ListThreads(
					context.Background(),
					actor,
				)
				if err != nil {
					t.Fatalf("list Threads after rejected binding: %v", err)
				}
				if len(threads) != 0 {
					t.Fatalf(
						"Thread count after rejected binding = %d, want 0",
						len(threads),
					)
				}
			} else {
				recovered, err := normalService.GetThread(
					context.Background(),
					actor,
					thread.ID,
				)
				if err != nil {
					t.Fatalf("recover Thread after rejected binding: %v", err)
				}
				if recovered.ActiveMatterID != "" {
					t.Fatalf(
						"active Matter after rejected binding = %q, want empty",
						recovered.ActiveMatterID,
					)
				}
			}
		})
	}
}

func TestPostgresAgentDataProtectedHTTP(t *testing.T) {
	database := newAgentTestDatabase(t)
	matterService, service := newAgentDataServices(t, database.pool)
	actors := map[string]requestcontext.Actor{
		"token-a": {
			UserID:    agentTestUserA,
			SessionID: "20000000-0000-4000-8000-000000000001",
		},
		"token-b": {
			UserID:    agentTestUserB,
			SessionID: "20000000-0000-4000-8000-000000000002",
		},
	}
	handler, err := NewHTTPHandler(
		service,
		matterService,
		authenticatorFunc(func(
			_ context.Context,
			token string,
		) (requestcontext.Actor, error) {
			actor, ok := actors[token]
			if !ok {
				return requestcontext.Actor{}, identity.ErrAuthenticationRequired
			}
			return actor, nil
		}),
		func() string { return "corr_agent_data_test" },
	)
	if err != nil {
		t.Fatalf("new HTTP handler: %v", err)
	}
	module, err := NewModule(handler)
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	module.RegisterRoutes(router)

	missingAuth := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/matters",
		"",
		"",
	)
	if missingAuth.Code != http.StatusUnauthorized ||
		missingAuth.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("missing auth response: %d %s", missingAuth.Code, missingAuth.Body)
	}

	forged := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Forged","owner_user_id":"`+agentTestUserB+`"}`,
		"token-a",
	)
	if forged.Code != http.StatusBadRequest {
		t.Fatalf("forged owner response: %d %s", forged.Code, forged.Body)
	}

	createdMatter := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/matters",
		`{"title":"Customer meeting"}`,
		"token-a",
	)
	if createdMatter.Code != http.StatusCreated {
		t.Fatalf(
			"create Matter response: %d %s",
			createdMatter.Code,
			createdMatter.Body,
		)
	}
	var matterBody struct {
		ID string `json:"matter_id"`
	}
	if err := json.Unmarshal(createdMatter.Body.Bytes(), &matterBody); err != nil {
		t.Fatalf("decode Matter response: %v", err)
	}
	nulMatter := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/matters",
		`{"title":"invalid\u0000title"}`,
		"token-a",
	)
	if nulMatter.Code != http.StatusBadRequest {
		t.Fatalf("NUL Matter response: %d %s", nulMatter.Code, nulMatter.Body)
	}
	recoveredMatter := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/matters/"+matterBody.ID,
		"",
		"token-a",
	)
	if recoveredMatter.Code != http.StatusOK ||
		!strings.Contains(recoveredMatter.Body.String(), `"title":"Customer meeting"`) {
		t.Fatalf(
			"recover Matter response: %d %s",
			recoveredMatter.Code,
			recoveredMatter.Body,
		)
	}
	privateMatter := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/matters/"+matterBody.ID,
		"",
		"token-b",
	)
	if privateMatter.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-user Matter response: %d %s",
			privateMatter.Code,
			privateMatter.Body,
		)
	}

	createdThread := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads",
		`{"active_matter_id":"`+matterBody.ID+`"}`,
		"token-a",
	)
	if createdThread.Code != http.StatusCreated {
		t.Fatalf(
			"create Thread response: %d %s",
			createdThread.Code,
			createdThread.Body,
		)
	}
	var threadBody struct {
		ID string `json:"thread_id"`
	}
	if err := json.Unmarshal(createdThread.Body.Bytes(), &threadBody); err != nil {
		t.Fatalf("decode Thread response: %v", err)
	}

	firstMessage := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		`{"client_message_id":"mobile-0001","content":"Help me prepare."}`,
		"token-a",
	)
	replayedMessage := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		`{"client_message_id":"mobile-0001","content":"Help me prepare."}`,
		"token-a",
	)
	if firstMessage.Code != http.StatusCreated ||
		replayedMessage.Code != http.StatusCreated ||
		!bytes.Equal(firstMessage.Body.Bytes(), replayedMessage.Body.Bytes()) {
		t.Fatalf(
			"idempotent HTTP responses differ: %d %s / %d %s",
			firstMessage.Code,
			firstMessage.Body,
			replayedMessage.Code,
			replayedMessage.Body,
		)
	}
	conflictingReplay := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		`{"client_message_id":"mobile-0001","content":"Changed content"}`,
		"token-a",
	)
	if conflictingReplay.Code != http.StatusConflict ||
		!strings.Contains(
			conflictingReplay.Body.String(),
			`"code":"idempotency_key_conflict"`,
		) {
		t.Fatalf(
			"idempotency conflict response: %d %s",
			conflictingReplay.Code,
			conflictingReplay.Body,
		)
	}
	nulMessage := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		`{"client_message_id":"mobile-nul","content":"invalid\u0000content"}`,
		"token-a",
	)
	if nulMessage.Code != http.StatusBadRequest {
		t.Fatalf("NUL Message response: %d %s", nulMessage.Code, nulMessage.Body)
	}
	maxEscapedMessage := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		`{"client_message_id":"mobile-max-escaped","content":"`+
			strings.Repeat(`\ud83d\ude00`, maxMessageContentRunes)+
			`"}`,
		"token-a",
	)
	if maxEscapedMessage.Code != http.StatusCreated {
		t.Fatalf(
			"maximum escaped Message response: %d %s",
			maxEscapedMessage.Code,
			maxEscapedMessage.Body,
		)
	}

	privateThread := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threadBody.ID,
		"",
		"token-b",
	)
	if privateThread.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-user Thread response: %d %s",
			privateThread.Code,
			privateThread.Body,
		)
	}
	privateMessages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		"",
		"token-b",
	)
	if privateMessages.Code != http.StatusNotFound {
		t.Fatalf(
			"cross-user Messages response: %d %s",
			privateMessages.Code,
			privateMessages.Body,
		)
	}
	messages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		"",
		"token-a",
	)
	if messages.Code != http.StatusOK ||
		!strings.Contains(messages.Body.String(), `"sequence":1`) {
		t.Fatalf("list messages response: %d %s", messages.Code, messages.Body)
	}
}

type authenticatorFunc func(
	context.Context,
	string,
) (requestcontext.Actor, error)

func (f authenticatorFunc) AuthenticateSession(
	ctx context.Context,
	token string,
) (requestcontext.Actor, error) {
	return f(ctx, token)
}

type idGeneratorFunc func() (string, error)

func (f idGeneratorFunc) NewID() (string, error) {
	return f()
}

type queryObservingPostgreSQL struct {
	*pgxpool.Pool
	observeQuery func(string)
}

func (database *queryObservingPostgreSQL) Begin(
	ctx context.Context,
) (pgx.Tx, error) {
	tx, err := database.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &queryObservingTx{
		Tx:           tx,
		observeQuery: database.observeQuery,
	}, nil
}

type queryObservingTx struct {
	pgx.Tx
	observeQuery func(string)
}

func (tx *queryObservingTx) QueryRow(
	ctx context.Context,
	query string,
	args ...any,
) pgx.Row {
	if tx.observeQuery != nil {
		tx.observeQuery(query)
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

type agentTestDatabase struct {
	pool      *pgxpool.Pool
	scopedURL string
}

func newAgentTestDatabase(t *testing.T) agentTestDatabase {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	admin, err := pgx.ConnectConfig(context.Background(), config)
	if err != nil {
		t.Fatal("connect to TEST_DATABASE_URL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "agent_data_" + hex.EncodeToString(random)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(
		context.Background(),
		"CREATE SCHEMA "+identifier,
	); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(
			context.Background(),
			"DROP SCHEMA "+identifier+" CASCADE",
		); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})

	scopedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL")
	}
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()

	runner, err := migration.Open(scopedURL.String())
	if err != nil {
		t.Fatalf("open migration runner: %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Close(); err != nil {
			t.Errorf("close migration runner: %v", err)
		}
	})
	if _, err := runner.Up(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(scopedURL.String())
	if err != nil {
		t.Fatal("parse scoped pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal("open scoped pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal("ping scoped pool")
	}
	for _, user := range []struct {
		id    string
		email string
	}{
		{id: agentTestUserA, email: "agent-a@example.com"},
		{id: agentTestUserB, email: "agent-b@example.com"},
	} {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO identity_users (id, canonical_email)
VALUES ($1, $2)`,
			user.id,
			user.email,
		); err != nil {
			t.Fatalf("insert identity user: %v", err)
		}
	}
	return agentTestDatabase{
		pool:      pool,
		scopedURL: scopedURL.String(),
	}
}

func (database agentTestDatabase) reopen(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(database.scopedURL)
	if err != nil {
		t.Fatal("parse reopened pool config")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal("reopen scoped pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal("ping reopened pool")
	}
	return pool
}

func newAgentDataServices(
	t *testing.T,
	pool *pgxpool.Pool,
) (*matter.Service, *Service) {
	t.Helper()
	ids := identity.NewUUIDv4Generator(nil)
	matterRepository, err := matter.NewPostgresRepository(pool, ids)
	if err != nil {
		t.Fatalf("new Matter repository: %v", err)
	}
	matterService, err := matter.NewService(matterRepository)
	if err != nil {
		t.Fatalf("new Matter service: %v", err)
	}
	repository, err := NewPostgresRepository(pool, ids)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	service, err := NewService(repository, matterService)
	if err != nil {
		t.Fatalf("new Agent service: %v", err)
	}
	return matterService, service
}

func assertCrossOwnerDatabaseConstraints(
	t *testing.T,
	pool *pgxpool.Pool,
	threadA string,
	threadB string,
	matterA string,
	matterB string,
) {
	t.Helper()
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_thread_matter_links (
    owner_user_id,
    thread_id,
    matter_id,
    is_active
) VALUES ($1, $2, $3, false)`,
		[]any{agentTestUserA, threadA, matterB},
		"23503",
		"agent_thread_matter_links_matter_owner_fkey",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content
) VALUES (
    '30000000-0000-4000-8000-000000000001',
    $1,
    $2,
    999,
    'user',
    'forged-owner',
    'must fail'
)`,
		[]any{agentTestUserA, threadB},
		"23503",
		"agent_messages_thread_owner_fkey",
	)
	assertPostgresConstraint(
		t,
		pool,
		`UPDATE agent_thread_matter_links
SET is_active = true
WHERE owner_user_id = $1 AND thread_id = $2 AND matter_id = $3`,
		[]any{agentTestUserA, threadA, matterA},
		"23505",
		"agent_thread_matter_links_one_active_idx",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content
) VALUES (
    '30000000-0000-4000-8000-000000000002',
    $1,
    $2,
    1000,
    'user',
    'client-message-1',
    'duplicate client identifier'
)`,
		[]any{agentTestUserA, threadA},
		"23505",
		"agent_messages_client_idempotency_key",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content
) VALUES (
    '30000000-0000-4000-8000-000000000003',
    $1,
    $2,
    1,
    'user',
    'duplicate-sequence',
    'duplicate sequence'
)`,
		[]any{agentTestUserA, threadA},
		"23505",
		"agent_messages_thread_sequence_key",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id,
    owner_user_id,
    thread_id,
    sequence_no,
    role,
    client_message_id,
    content
) VALUES (
    '30000000-0000-4000-8000-000000000004',
    $1,
    $2,
    1001,
    'user',
    'oversized-content',
    $3
)`,
		[]any{agentTestUserA, threadA, strings.Repeat("x", 4097)},
		"23514",
		"agent_messages_content_length_check",
	)
}

func assertRestrictedCrossModuleDeletes(
	t *testing.T,
	pool *pgxpool.Pool,
	matterID string,
) {
	t.Helper()
	assertPostgresConstraint(
		t,
		pool,
		"DELETE FROM matters WHERE id = $1",
		[]any{matterID},
		"23001",
		"agent_thread_matter_links_matter_owner_fkey",
	)
	assertPostgresConstraint(
		t,
		pool,
		"DELETE FROM identity_users WHERE id = $1",
		[]any{agentTestUserA},
		"23001",
		"matters_owner_user_id_fkey",
	)
}

func assertPostgresConstraint(
	t *testing.T,
	pool *pgxpool.Pool,
	statement string,
	arguments []any,
	code string,
	constraint string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), statement, arguments...)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded; want %s", constraint)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) ||
		postgresError.Code != code ||
		postgresError.ConstraintName != constraint {
		t.Fatalf(
			"statement error = %v, want %s/%s",
			err,
			code,
			constraint,
		)
	}
}

func performAgentRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	token string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
