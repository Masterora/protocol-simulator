package tui

import (
	"context"
	"os"
)

func readKeys(_ context.Context, _ *os.File, keys chan<- byte) { close(keys) }
