package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene/ielts"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/database"
)

func main() {
	if err := config.LoadDotEnvUpwards(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load .env failed: %v\n", err)
		os.Exit(1)
	}
	if err := execute(os.Args[1:], os.Getenv("DATABASE_URL")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "IELTS question bank import failed: %v\n", err)
		os.Exit(1)
	}
}

func execute(args []string, databaseURL string) error {
	flags := flag.NewFlagSet("ielts-bank-import", flag.ContinueOnError)
	filePath := flags.String("file", "", "path to a version 3 IELTS question bank JSON file")
	publish := flags.Bool("publish", false, "publish the imported version atomically")
	publishIfEmpty := flags.Bool(
		"publish-if-empty",
		false,
		"publish only when the region has no published question bank",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *filePath == "" || flags.NArg() != 0 {
		return errors.New("usage: ielts-bank-import -file PATH [-publish | -publish-if-empty]")
	}
	if *publish && *publishIfEmpty {
		return errors.New("-publish and -publish-if-empty are mutually exclusive")
	}
	input, err := os.Open(*filePath)
	if err != nil {
		return fmt.Errorf("open import file: %w", err)
	}
	defer input.Close()
	document, err := ielts.DecodeImportDocument(input)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	importer, err := ielts.NewImporter(pool.Native())
	if err != nil {
		return err
	}
	if *publishIfEmpty {
		exists, err := importer.HasPublishedBank(ctx, document.Region)
		if err != nil {
			return err
		}
		if exists {
			_, err = fmt.Printf("bank=%s skipped=true reason=published_bank_exists\n", document.BankID)
			return err
		}
	}
	result, err := importer.Import(ctx, document, *publish || *publishIfEmpty)
	if err != nil {
		return err
	}
	_, err = fmt.Printf(
		"bank=%s published=%t part1_topics=%d part1_questions=%d part1_sets=%d topic_groups=%d part3_questions=%d\n",
		result.BankID,
		result.Published,
		result.Part1Topics,
		result.Part1Questions,
		result.Part1Sets,
		result.TopicGroups,
		result.Part3Questions,
	)
	return err
}
