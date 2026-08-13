package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIELTSResolverProducesPlanCompatibleAssignments(t *testing.T) {
	pool := planIELTSTestDatabase(t)
	importer, err := ielts.NewImporter(pool)
	if err != nil {
		t.Fatalf("NewImporter: %v", err)
	}
	input, err := os.Open("../../../../data/ielts/2026-05-08-mainland.json")
	if err != nil {
		t.Fatalf("open current IELTS question bank: %v", err)
	}
	defer input.Close()
	document, err := ielts.DecodeImportDocument(input)
	if err != nil {
		t.Fatalf("DecodeImportDocument: %v", err)
	}
	if _, err := importer.Import(context.Background(), document, true); err != nil {
		t.Fatalf("Import: %v", err)
	}
	resolver, err := ielts.NewPostgresStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	part1, err := resolver.AssignQuestionSet(
		context.Background(),
		ielts.PracticeModePart1,
		"person",
	)
	if err != nil || len(part1.Parts) != 1 ||
		part1.Parts[0].TopicTitle != "老师" {
		t.Fatalf("resolve Part 1 topic title = (%#v, %v)", part1, err)
	}

	topicGroupID := document.TopicGroups[0].ID
	assertCompatible := func(
		t *testing.T,
		mode scene.PracticeMode,
		request *IELTSQuestionSelection,
	) {
		t.Helper()
		selection := planIELTSSelectionFixture()
		selection.Scene.PracticeOptions[0].Mode = mode
		frozen, assignment, err := freezeIELTSAssignment(
			context.Background(),
			resolver,
			selection,
			request,
		)
		if err != nil {
			t.Fatalf("freezeIELTSAssignment: %v", err)
		}
		if !ValidPlanIELTSAssignment(frozen, assignment) {
			t.Fatalf("PostgreSQL assignment is incompatible with Plan contract: %#v", assignment)
		}
	}

	t.Run("full mock assignment", func(t *testing.T) {
		assertCompatible(t, scene.PracticeModeFullMock, nil)
	})
	for _, mode := range []scene.PracticeMode{
		scene.PracticeModePart1,
		scene.PracticeModePart2,
		scene.PracticeModePart3,
	} {
		t.Run(string(mode)+" random assignment", func(t *testing.T) {
			assertCompatible(t, mode, nil)
		})
	}
	for _, topic := range document.Part1Topics {
		t.Run("Part 1 topic/"+topic.ID, func(t *testing.T) {
			assertCompatible(
				t,
				scene.PracticeModePart1,
				&IELTSQuestionSelection{Part1SetID: topic.ID},
			)
		})
	}
	for _, test := range []struct {
		name    string
		mode    scene.PracticeMode
		request IELTSQuestionSelection
	}{
		{
			name:    "Part 2",
			mode:    scene.PracticeModePart2,
			request: IELTSQuestionSelection{TopicGroupID: topicGroupID},
		},
		{
			name:    "Part 3",
			mode:    scene.PracticeModePart3,
			request: IELTSQuestionSelection{TopicGroupID: topicGroupID},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertCompatible(t, test.mode, &test.request)
		})
	}

	for _, cueCardType := range []string{
		"person",
		"place",
		"thing",
		"experience",
	} {
		for _, mode := range []scene.PracticeMode{
			scene.PracticeModePart1,
			scene.PracticeModePart2,
			scene.PracticeModePart3,
		} {
			t.Run(string(mode)+"/"+cueCardType, func(t *testing.T) {
				selection := planIELTSSelectionFixture()
				selection.Scene.PracticeOptions[0].Mode = mode
				frozen, assignment, err := freezeIELTSAssignment(
					context.Background(),
					resolver,
					selection,
					&IELTSQuestionSelection{CueCardType: cueCardType},
				)
				if err != nil {
					t.Fatalf("freezeIELTSAssignment: %v", err)
				}
				if !ValidPlanIELTSAssignment(frozen, assignment) {
					t.Fatalf("invalid category assignment: %#v", assignment)
				}
				if mode == scene.PracticeModePart1 &&
					assignment.Parts[0].TopicTitle != "" {
					t.Fatalf(
						"Part 1 assignment persisted topic title %q",
						assignment.Parts[0].TopicTitle,
					)
				}
				var assignedType string
				sourceID := assignment.Parts[0].SourceID
				table := "ielts_part23_groups"
				idColumn := "topic_group_id"
				if mode == scene.PracticeModePart1 {
					table = "ielts_part1_topics"
					idColumn = "topic_id"
				}
				query := "SELECT cue_card_type FROM " + table +
					" WHERE bank_id=$1 AND " + idColumn + "=$2"
				if err := pool.QueryRow(
					context.Background(),
					query,
					assignment.BankID,
					sourceID,
				).Scan(&assignedType); err != nil {
					t.Fatalf("read assigned Cue Card type: %v", err)
				}
				if assignedType != cueCardType {
					t.Fatalf(
						"assigned Cue Card type = %q, want %q",
						assignedType,
						cueCardType,
					)
				}
			})
		}
	}
}

func planIELTSTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open IELTS Plan test database: %v", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		admin.Close()
		t.Fatalf("create IELTS Plan schema suffix: %v", err)
	}
	schema := "ielts_plan_" + hex.EncodeToString(suffix[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatalf("create IELTS Plan schema: %v", err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
		t.Fatalf("parse IELTS Plan database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
		t.Fatalf("open isolated IELTS Plan pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop IELTS Plan schema: %v", err)
		}
		admin.Close()
	})
	for _, name := range []string{
		"000080_ielts_versioned_question_bank.up.sql",
		"000087_ielts_part1_cue_card_types.up.sql",
	} {
		migration, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read IELTS question bank migration %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply IELTS question bank migration %s: %v", name, err)
		}
	}
	return pool
}
