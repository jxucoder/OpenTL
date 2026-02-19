// Package memory provides cross-session memory for TeleCoder.
// It stores session summary embeddings and retrieves relevant past sessions
// to inject as context when new sessions start.
package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Summary represents a stored session summary with its embedding.
type Summary struct {
	SessionID string
	Repo      string
	Prompt    string
	Result    string
	Embedding []float64
	CreatedAt time.Time
}

// Match is a retrieval result with its similarity score.
type Match struct {
	Summary    Summary
	Similarity float64
}

// Store provides cross-session memory with vector search.
type Store struct {
	mu        sync.RWMutex
	summaries []Summary
	embedder  Embedder
}

// New creates a new memory Store with the given embedder.
func New(embedder Embedder) *Store {
	return &Store{
		embedder: embedder,
	}
}

// Add stores a session summary with its embedding.
func (s *Store) Add(ctx context.Context, sessionID, repo, prompt, result string) error {
	text := fmt.Sprintf("Repo: %s\nTask: %s\nResult: %s", repo, prompt, result)
	embedding, err := s.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("generating embedding: %w", err)
	}

	summary := Summary{
		SessionID: sessionID,
		Repo:      repo,
		Prompt:    prompt,
		Result:    result,
		Embedding: embedding,
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	s.summaries = append(s.summaries, summary)
	s.mu.Unlock()

	return nil
}

// Query retrieves the top-k most relevant past sessions for the given repo and prompt.
// Only sessions from the same repo are returned.
func (s *Store) Query(ctx context.Context, repo, prompt string, topK int) ([]Match, error) {
	queryEmbedding, err := s.embedder.Embed(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generating query embedding: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matches []Match
	for _, sum := range s.summaries {
		if sum.Repo != repo {
			continue
		}
		sim := cosineSimilarity(queryEmbedding, sum.Embedding)
		matches = append(matches, Match{Summary: sum, Similarity: sim})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	if len(matches) > topK {
		matches = matches[:topK]
	}

	return matches, nil
}

// Count returns the total number of stored summaries.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.summaries)
}

// FormatContext builds a markdown context string from retrieved matches
// suitable for injecting into a session prompt.
func FormatContext(matches []Match) string {
	if len(matches) == 0 {
		return ""
	}
	result := "## Relevant Past Sessions\n\n"
	for i, m := range matches {
		result += fmt.Sprintf("%d. **%s** (similarity: %.2f)\n   Task: %s\n   Result: %s\n\n",
			i+1, m.Summary.SessionID, m.Similarity, m.Summary.Prompt, truncate(m.Summary.Result, 200))
	}
	return result
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
