package postgres_test

import (
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

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	conversationhttp "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/http"
	conversationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/identity"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/httpresponse"
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
	agentTestUserC = "10000000-0000-4000-8000-000000000003"
)

func TestPostgresAgentConversationVerticalSlice(t *testing.T) {
	database := newAgentTestDatabase(t)
	service := newAgentDataServices(t, database.pool)
	actorA := requestcontext.Actor{
		UserID:    agentTestUserA,
		SessionID: "20000000-0000-4000-8000-000000000001",
	}
	actorB := requestcontext.Actor{
		UserID:    agentTestUserB,
		SessionID: "20000000-0000-4000-8000-000000000002",
	}

	threadA, err := service.CreateThread(context.Background(), actorA)
	if err != nil {
		t.Fatalf("create thread A: %v", err)
	}
	threadB, err := service.CreateThread(context.Background(), actorB)
	if err != nil {
		t.Fatalf("create thread B: %v", err)
	}
	if _, err := service.GetThread(
		context.Background(),
		actorA,
		threadB.ID,
	); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-owner Thread read error = %v, want not found", err)
	}
	if _, err := service.AppendUserMessage(
		context.Background(),
		actorA,
		threadB.ID,
		"cross-owner-message",
		"must not be stored",
	); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-owner Message write error = %v, want not found", err)
	}
	if _, err := service.ListMessages(
		context.Background(),
		actorA,
		threadB.ID,
	); !errors.Is(err, conversation.ErrNotFound) {
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
	); !errors.Is(err, conversation.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v, want idempotency conflict", err)
	}

	const concurrentMessages = 16
	start := make(chan struct{})
	results := make(chan conversation.Message, concurrentMessages)
	failures := make(chan error, concurrentMessages)
	var writers sync.WaitGroup
	for index := range concurrentMessages {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			message, appendErr := service.AppendUserMessage(
				context.Background(),
				actorA,
				threadA.ID,
				fmt.Sprintf("parallel-%02d", index),
				fmt.Sprintf("parallel content %02d", index),
			)
			if appendErr != nil {
				failures <- appendErr
				return
			}
			results <- message
		}()
	}
	close(start)
	writers.Wait()
	close(results)
	close(failures)
	for failure := range failures {
		t.Errorf("parallel append: %v", failure)
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
		if want := int64(index + 1); sequence != want {
			t.Fatalf("sequence[%d] = %d, want %d", index, sequence, want)
		}
	}

	panicThread, err := service.CreateThread(context.Background(), actorA)
	if err != nil {
		t.Fatalf("create panic rollback thread: %v", err)
	}
	panicRepository, err := conversationpostgres.New(
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
		defer func() { recovered = recover() }()
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
	if acquiredAfterPanic := database.pool.Stat().AcquiredConns(); acquiredAfterPanic != acquiredBeforePanic {
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

	assertConversationDatabaseConstraints(t, database.pool, threadA.ID)
	messages, err := service.ListMessages(context.Background(), actorA, threadA.ID)
	if err != nil {
		t.Fatalf("list messages before reconnect: %v", err)
	}
	if len(messages) != concurrentMessages+1 {
		t.Fatalf("message count = %d, want %d", len(messages), concurrentMessages+1)
	}
	database.pool.Close()
	reopenedPool := database.reopen(t)
	recoveredService := newAgentDataServices(t, reopenedPool)
	if _, err := recoveredService.GetThread(
		context.Background(),
		actorA,
		threadA.ID,
	); err != nil {
		t.Fatalf("recover Thread: %v", err)
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

func TestPostgresAgentConversationProtectedHTTP(t *testing.T) {
	database := newAgentTestDatabase(t)
	service := newAgentDataServices(t, database.pool)
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
	renderer := httpresponse.NewRenderer(func() string { return "corr_agent_data_test" })
	handler, err := conversationhttp.NewHandler(service, renderer)
	if err != nil {
		t.Fatalf("new Conversation HTTP handler: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("")
	protected.Use(func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if actor, ok := actors[token]; ok {
			c.Request = c.Request.WithContext(
				requestcontext.WithActor(c.Request.Context(), actor),
			)
		}
		c.Next()
	})
	handler.RegisterRoutes(protected)

	missingAuth := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads",
		"",
		"",
	)
	if missingAuth.Code != http.StatusUnauthorized ||
		missingAuth.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("missing auth response: %d %s", missingAuth.Code, missingAuth.Body)
	}
	createdThread := performAgentRequest(
		router,
		http.MethodPost,
		"/v1/agent-threads",
		"",
		"token-a",
	)
	if createdThread.Code != http.StatusCreated {
		t.Fatalf("create Thread response: %d %s", createdThread.Code, createdThread.Body)
	}
	var threadBody struct {
		ID string `json:"thread_id"`
	}
	if err := json.Unmarshal(createdThread.Body.Bytes(), &threadBody); err != nil {
		t.Fatalf("decode Thread response: %v", err)
	}
	if _, err := service.AppendUserMessage(
		context.Background(),
		actors["token-a"],
		threadBody.ID,
		"seed-message-0001",
		"Seed one committed Message for the read contract.",
	); err != nil {
		t.Fatalf("seed Message for read contract: %v", err)
	}
	privateThread := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threadBody.ID,
		"",
		"token-b",
	)
	if privateThread.Code != http.StatusNotFound {
		t.Fatalf("cross-user Thread response: %d %s", privateThread.Code, privateThread.Body)
	}
	privateMessages := performAgentRequest(
		router,
		http.MethodGet,
		"/v1/agent-threads/"+threadBody.ID+"/messages",
		"",
		"token-b",
	)
	if privateMessages.Code != http.StatusNotFound {
		t.Fatalf("cross-user Messages response: %d %s", privateMessages.Code, privateMessages.Body)
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

type idGeneratorFunc func() (string, error)

func (generator idGeneratorFunc) NewID() (string, error) {
	return generator()
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
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+identifier); err != nil {
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
		{id: agentTestUserC, email: "agent-c@example.com"},
	} {
		if _, err := pool.Exec(
			context.Background(),
			`INSERT INTO users (id, canonical_email) VALUES ($1, $2)`,
			user.id,
			user.email,
		); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}
	return agentTestDatabase{pool: pool, scopedURL: scopedURL.String()}
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

func newAgentDataServices(t *testing.T, pool *pgxpool.Pool) *conversation.Service {
	t.Helper()
	repository, err := conversationpostgres.New(
		pool,
		identity.NewUUIDv4Generator(nil),
	)
	if err != nil {
		t.Fatalf("new Agent repository: %v", err)
	}
	service, err := conversation.NewService(repository)
	if err != nil {
		t.Fatalf("new Agent service: %v", err)
	}
	return service
}

func assertConversationDatabaseConstraints(
	t *testing.T,
	pool *pgxpool.Pool,
	threadA string,
) {
	t.Helper()
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, client_message_id, content
) VALUES (
    '30000000-0000-4000-8000-000000000002', $1, 1000, 'user',
    'client-message-1', 'duplicate client identifier'
)`,
		[]any{threadA},
		"23505",
		"agent_messages_client_idempotency_key",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, client_message_id, content
) VALUES (
    '30000000-0000-4000-8000-000000000003', $1, 1, 'user',
    'duplicate-sequence', 'duplicate sequence'
)`,
		[]any{threadA},
		"23505",
		"agent_messages_thread_sequence_key",
	)
	assertPostgresConstraint(
		t,
		pool,
		`INSERT INTO agent_messages (
    id, thread_id, sequence_no, role, client_message_id, content
) VALUES (
    '30000000-0000-4000-8000-000000000004', $1, 1001, 'user',
    'oversized-content', $2
)`,
		[]any{threadA, strings.Repeat("x", 4097)},
		"23514",
		"agent_messages_content_check",
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
		t.Fatalf("statement error = %v, want %s/%s", err, code, constraint)
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
