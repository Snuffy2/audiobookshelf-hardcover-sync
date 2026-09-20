package models

import (
	"context"
	"strings"
)

// Reading formats an Audiobookshelf item, and the Hardcover edition matching it,
// can have.
const (
	ReadingFormatAudiobook = "audiobook"
	ReadingFormatEbook     = "ebook"
)

type readingFormatKey struct{}

// WithReadingFormat returns a context that carries the desired reading format
// ("audiobook" or "ebook", case-insensitive). Hardcover lookups made with it
// only consider editions of that format; without one they default to audiobook.
func WithReadingFormat(ctx context.Context, format string) context.Context {
	return context.WithValue(ctx, readingFormatKey{}, strings.ToLower(strings.TrimSpace(format)))
}

// ReadingFormatFromContext returns the reading format carried by ctx, if any.
func ReadingFormatFromContext(ctx context.Context) (string, bool) {
	if s, ok := ctx.Value(readingFormatKey{}).(string); ok && s != "" {
		return s, true
	}
	return "", false
}
