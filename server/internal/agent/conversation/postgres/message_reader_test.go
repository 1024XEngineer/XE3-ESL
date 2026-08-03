package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/agent/conversation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFindMessageReadsOwnedConversationMessage(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.August, 3, 10, 30, 0, 0, time.UTC)
	database := &messageReaderDatabase{
		row: messageReaderRow(func(destinations ...any) error {
			*(destinations[0].(*string)) = "40000000-0000-4000-8000-000000000001"
			*(destinations[1].(*string)) = "20000000-0000-4000-8000-000000000001"
			*(destinations[2].(*string)) = "30000000-0000-4000-8000-000000000001"
			*(destinations[3].(*int64)) = 2
			*(destinations[4].(*string)) = "assistant"
			*(destinations[5].(*string)) = ""
			*(destinations[6].(*string)) = "10000000-0000-4000-8000-000000000001"
			*(destinations[7].(*string)) = "text"
			*(destinations[8].(*string)) = "I will tailor the practice."
			*(destinations[9].(*time.Time)) = createdAt
			return nil
		}),
	}
	repository := &Repository{database: database}

	message, err := repository.FindMessage(
		context.Background(),
		"20000000-0000-4000-8000-000000000001",
		"30000000-0000-4000-8000-000000000001",
		"40000000-0000-4000-8000-000000000001",
	)
	if err != nil {
		t.Fatalf("FindMessage: %v", err)
	}
	if message.ID != "40000000-0000-4000-8000-000000000001" ||
		message.OwnerID != "20000000-0000-4000-8000-000000000001" ||
		message.ThreadID != "30000000-0000-4000-8000-000000000001" ||
		message.Role != conversation.MessageRoleAssistant ||
		message.Modality != conversation.MessageModalityText ||
		message.ProducedByRunID != "10000000-0000-4000-8000-000000000001" ||
		message.Content != "I will tailor the practice." ||
		!message.CreatedAt.Equal(createdAt) {
		t.Fatalf("message = %#v", message)
	}
	wantArguments := []any{
		"40000000-0000-4000-8000-000000000001",
		"20000000-0000-4000-8000-000000000001",
		"30000000-0000-4000-8000-000000000001",
	}
	if !reflect.DeepEqual(database.arguments, wantArguments) {
		t.Fatalf("query arguments = %#v, want %#v", database.arguments, wantArguments)
	}
	if !strings.Contains(database.query, "FROM agent_messages") ||
		!strings.Contains(database.query, "owner_user_id = $2") ||
		!strings.Contains(database.query, "thread_id = $3") {
		t.Fatalf("query does not enforce Conversation ownership: %s", database.query)
	}
}

func TestFindMessageMapsPostgresReadErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		readError error
		want      error
	}{
		"not found": {
			readError: pgx.ErrNoRows,
			want:      conversation.ErrNotFound,
		},
		"repository failure": {
			readError: errors.New("database unavailable"),
			want:      conversation.ErrRepository,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repository := &Repository{database: &messageReaderDatabase{
				row: messageReaderRow(func(...any) error {
					return test.readError
				}),
			}}
			_, err := repository.FindMessage(
				context.Background(),
				"20000000-0000-4000-8000-000000000001",
				"30000000-0000-4000-8000-000000000001",
				"40000000-0000-4000-8000-000000000001",
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("FindMessage error = %v, want %v", err, test.want)
			}
		})
	}
}

type messageReaderDatabase struct {
	row       pgx.Row
	query     string
	arguments []any
}

func (*messageReaderDatabase) Begin(context.Context) (pgx.Tx, error) {
	panic("unexpected Begin")
}

func (*messageReaderDatabase) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (*messageReaderDatabase) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (database *messageReaderDatabase) QueryRow(
	_ context.Context,
	query string,
	arguments ...any,
) pgx.Row {
	database.query = query
	database.arguments = append([]any(nil), arguments...)
	return database.row
}

type messageReaderRow func(...any) error

func (row messageReaderRow) Scan(destinations ...any) error {
	return row(destinations...)
}
