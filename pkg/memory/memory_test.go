package memory

import (
	"context"
	"math"
	"testing"
)

// mockEmbedder returns deterministic embeddings based on keyword overlap.
// Words are hashed to vector positions for simple but effective similarity.
type mockEmbedder struct{}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	vec := make([]float64, 64)
	for i, c := range text {
		vec[(int(c)+i)%64] += 1.0
	}
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec, nil
}

func TestAddAndQuery(t *testing.T) {
	ctx := context.Background()
	s := New(&mockEmbedder{})

	sessions := []struct {
		id, repo, prompt, result string
	}{
		{"s1", "org/api", "add rate limiting", "Added rate limiter middleware"},
		{"s2", "org/api", "fix auth bug", "Fixed JWT validation"},
		{"s3", "org/api", "add user pagination", "Added pagination to /users"},
		{"s4", "org/api", "update rate limits", "Updated rate limit thresholds"},
		{"s5", "org/api", "add caching", "Added Redis caching layer"},
		{"s6", "org/api", "fix pagination bug", "Fixed off-by-one in pagination"},
		{"s7", "org/api", "add logging middleware", "Added structured logging"},
		{"s8", "org/api", "refactor auth module", "Refactored authentication"},
		{"s9", "org/api", "add health check endpoint", "Added /health endpoint"},
		{"s10", "org/api", "update dependencies", "Updated Go modules"},
	}

	for _, sess := range sessions {
		if err := s.Add(ctx, sess.id, sess.repo, sess.prompt, sess.result); err != nil {
			t.Fatalf("Add(%s): %v", sess.id, err)
		}
	}

	if s.Count() != 10 {
		t.Fatalf("expected 10 summaries, got %d", s.Count())
	}

	matches, err := s.Query(ctx, "org/api", "rate limiting configuration", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}

	for _, m := range matches {
		if m.Similarity <= 0 {
			t.Fatalf("expected positive similarity, got %f", m.Similarity)
		}
	}

	if matches[0].Similarity < matches[1].Similarity {
		t.Fatal("matches should be sorted by similarity descending")
	}
}

func TestQuery_DifferentRepoNotReturned(t *testing.T) {
	ctx := context.Background()
	s := New(&mockEmbedder{})

	s.Add(ctx, "s1", "org/api", "add feature", "done")
	s.Add(ctx, "s2", "org/web", "add feature", "done")
	s.Add(ctx, "s3", "other/repo", "add feature", "done")

	matches, err := s.Query(ctx, "org/api", "add feature", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("expected 1 match (same repo only), got %d", len(matches))
	}
	if matches[0].Summary.Repo != "org/api" {
		t.Fatalf("expected repo 'org/api', got %q", matches[0].Summary.Repo)
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{1, 0, 0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 0.001 {
		t.Fatalf("identical vectors should have similarity 1.0, got %f", sim)
	}

	c := []float64{0, 1, 0}
	sim = cosineSimilarity(a, c)
	if math.Abs(sim) > 0.001 {
		t.Fatalf("orthogonal vectors should have similarity 0.0, got %f", sim)
	}

	d := []float64{0.7, 0.7, 0}
	sim = cosineSimilarity(a, d)
	if sim < 0.7 {
		t.Fatalf("expected similarity > 0.7, got %f", sim)
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	sim := cosineSimilarity([]float64{}, []float64{})
	if sim != 0 {
		t.Fatalf("expected 0 for empty vectors, got %f", sim)
	}

	sim = cosineSimilarity([]float64{1, 2}, []float64{1})
	if sim != 0 {
		t.Fatalf("expected 0 for mismatched lengths, got %f", sim)
	}
}

func TestFormatContext(t *testing.T) {
	matches := []Match{
		{Summary: Summary{SessionID: "s1", Prompt: "add rate limiting", Result: "Added middleware"}, Similarity: 0.95},
		{Summary: Summary{SessionID: "s2", Prompt: "fix auth", Result: "Fixed JWT"}, Similarity: 0.82},
	}

	ctx := FormatContext(matches)
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}
	if len(ctx) < 20 {
		t.Fatalf("context too short: %q", ctx)
	}
}

func TestFormatContext_Empty(t *testing.T) {
	ctx := FormatContext(nil)
	if ctx != "" {
		t.Fatalf("expected empty string for no matches, got %q", ctx)
	}
}

func TestQuery_TopK(t *testing.T) {
	ctx := context.Background()
	s := New(&mockEmbedder{})

	for i := 0; i < 20; i++ {
		s.Add(ctx, "s"+string(rune('A'+i)), "org/api", "task", "result")
	}

	matches, _ := s.Query(ctx, "org/api", "task", 5)
	if len(matches) != 5 {
		t.Fatalf("expected 5 matches (topK=5), got %d", len(matches))
	}
}

func TestSimilarPrompts_HighSimilarity(t *testing.T) {
	ctx := context.Background()
	e := &mockEmbedder{}

	v1, _ := e.Embed(ctx, "add rate limiting to API")
	v2, _ := e.Embed(ctx, "add rate limiting to API endpoints")
	sim := cosineSimilarity(v1, v2)
	if sim < 0.7 {
		t.Fatalf("expected similarity > 0.7 for related prompts, got %f", sim)
	}
}
