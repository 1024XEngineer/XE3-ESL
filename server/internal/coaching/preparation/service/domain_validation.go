package service

import . "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation"

func validIdempotencyKey(value string) bool { return ValidIdempotencyKey(value) }
