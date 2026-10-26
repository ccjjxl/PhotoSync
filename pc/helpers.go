package main

import (
	"context"
	"time"
)

func mustCtx() context.Context { return context.Background() }

func nowUnix() int64 { return time.Now().Unix() }
