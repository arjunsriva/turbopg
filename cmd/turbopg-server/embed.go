package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/arjunsriva/turbopg"
)

var errEmbeddingsNotConfigured = turbopg.InvalidInput("native embeddings are not configured")

// Embedder turns texts into dense vectors for a named model.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string, dims int) ([][]float32, error)
}

func parseEmbedSpecs(schema map[string]interface{}) []turbopg.EmbedSpec {
	return turbopg.EmbedSpecs(schema)
}

func (s *Server) loadEmbedSpecs(ctx context.Context, namespace string, reqSchema map[string]interface{}) []turbopg.EmbedSpec {
	merged := map[string]interface{}{}
	if s.Store != nil {
		if stored, err := s.Store.GetSchema(ctx, namespace); err == nil {
			for k, v := range stored {
				merged[k] = v
			}
		}
	}
	for k, v := range reqSchema {
		merged[k] = v
	}
	return parseEmbedSpecs(merged)
}

func (s *Server) namespaceDimsForCreate(ctx context.Context, schema map[string]interface{}, docs, patchDocs []turbopg.Document) (int, error) {
	if v := firstVectorFromDocs(docs); len(v) > 0 {
		return len(v), nil
	}
	if v := firstVectorFromDocs(patchDocs); len(v) > 0 {
		return len(v), nil
	}
	specs := parseEmbedSpecs(schema)
	if len(specs) == 0 {
		return 0, nil
	}
	primary := 0
	for _, spec := range specs {
		if spec.ComputedAttr() == "" {
			primary++
			if spec.Dims > 0 {
				return spec.Dims, nil
			}
		}
	}
	if primary > 1 {
		return 0, turbopg.InvalidInput("only one embed field may target the vector column")
	}
	if len(specs) > turbopg.MaxEmbedFields {
		return 0, turbopg.InvalidInputf("at most %d embedded attributes are supported", turbopg.MaxEmbedFields)
	}
	spec := specs[0]
	if spec.Dims > 0 {
		return spec.Dims, nil
	}
	if isDemoModel(spec.Model) {
		return 0, turbopg.InvalidInputf("embedding model %q is not supported", spec.Model)
	}
	if s.Embedder == nil {
		return 0, errEmbeddingsNotConfigured
	}
	text := firstEmbedText(docs, spec)
	if text == "" {
		text = firstEmbedText(patchDocs, spec)
	}
	if text == "" {
		return 0, turbopg.InvalidInputf("native embeddings require embed.dims or documents with %s", spec.Field)
	}
	vecs, err := s.Embedder.Embed(ctx, spec.Model, []string{text}, 0)
	if err != nil {
		return 0, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return 0, fmt.Errorf("embedding provider returned an empty vector")
	}
	return len(vecs[0]), nil
}

func firstEmbedText(docs []turbopg.Document, spec turbopg.EmbedSpec) string {
	for _, doc := range docs {
		if text := attrString(doc.Attributes[spec.Field]); text != "" {
			return text
		}
	}
	return ""
}

func (s *Server) applyEmbeddings(ctx context.Context, docs []turbopg.Document, specs []turbopg.EmbedSpec) error {
	if len(docs) == 0 || len(specs) == 0 {
		return nil
	}
	if len(specs) > turbopg.MaxEmbedFields {
		return turbopg.InvalidInputf("at most %d embedded attributes are supported", turbopg.MaxEmbedFields)
	}
	for _, spec := range specs {
		if err := s.applyOneEmbed(ctx, docs, spec); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) applyOneEmbed(ctx context.Context, docs []turbopg.Document, spec turbopg.EmbedSpec) error {
	if spec.Model == "" {
		return turbopg.InvalidInput("native embeddings require embed.model")
	}
	if isDemoModel(spec.Model) {
		return turbopg.InvalidInputf("embedding model %q is not supported", spec.Model)
	}

	var texts []string
	var idxs []int
	for i := range docs {
		if spec.ComputedAttr() == "" && len(docs[i].Vector) > 0 {
			continue
		}
		if dest := spec.ComputedAttr(); dest != "" && docs[i].ExtraVectors != nil {
			if len(docs[i].ExtraVectors[dest]) > 0 {
				continue
			}
		}
		text := attrString(docs[i].Attributes[spec.Field])
		if text == "" {
			continue
		}
		texts = append(texts, text)
		idxs = append(idxs, i)
	}
	if len(texts) == 0 {
		return nil
	}
	if s.Embedder == nil {
		return errEmbeddingsNotConfigured
	}
	vecs, err := s.Embedder.Embed(ctx, spec.Model, texts, spec.Dims)
	if err != nil {
		return err
	}
	if len(vecs) != len(texts) {
		return fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(vecs), len(texts))
	}
	dest := spec.ComputedAttr()
	for j, i := range idxs {
		if spec.Dims > 0 && len(vecs[j]) != spec.Dims {
			return turbopg.InvalidInputf("embedding dimensions mismatch: got %d, want %d", len(vecs[j]), spec.Dims)
		}
		if dest == "" {
			docs[i].Vector = vecs[j]
			continue
		}
		if docs[i].ExtraVectors == nil {
			docs[i].ExtraVectors = map[string][]float32{}
		}
		docs[i].ExtraVectors[dest] = vecs[j]
		if docs[i].Attributes == nil {
			docs[i].Attributes = map[string]interface{}{}
		}
		docs[i].Attributes[dest] = vecs[j]
		if len(docs[i].Vector) == 0 {
			docs[i].Vector = vecs[j]
		}
	}
	return nil
}

func (s *Server) embedPatchAttrs(ctx context.Context, attrs map[string]interface{}, specs []turbopg.EmbedSpec) ([]float32, map[string]interface{}, error) {
	if len(attrs) == 0 || len(specs) == 0 {
		return nil, attrs, nil
	}
	if len(specs) > turbopg.MaxEmbedFields {
		return nil, nil, turbopg.InvalidInputf("at most %d embedded attributes are supported", turbopg.MaxEmbedFields)
	}
	docs := []turbopg.Document{{ID: "_", Attributes: attrs}}
	if err := s.applyEmbeddings(ctx, docs, specs); err != nil {
		return nil, nil, err
	}
	return docs[0].Vector, docs[0].Attributes, nil
}

func hasClientVector(docs []turbopg.Document) bool {
	for _, doc := range docs {
		if len(doc.Vector) > 0 {
			return true
		}
	}
	return false
}

func attrString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func isDemoModel(model string) bool {
	return strings.EqualFold(model, "example/random") || strings.HasPrefix(strings.ToLower(model), "example/")
}

func (s *Server) resolveEmbedQuery(ctx context.Context, spec turbopg.RankSpec, schema map[string]interface{}, dims int) (turbopg.RankSpec, error) {
	if spec.EmbedQuery == "" {
		return spec, nil
	}
	model := spec.EmbedModel
	embedDims := dims
	if model == "" {
		for _, s := range parseEmbedSpecs(schema) {
			if spec.Field == "vector" || s.Field == spec.Field {
				model = s.Model
				if s.Dims > 0 {
					embedDims = s.Dims
				}
				if spec.Field != "vector" && s.Field == spec.Field {
					break
				}
			}
		}
	}
	if model == "" {
		return spec, turbopg.InvalidInput("Embed requires a model")
	}
	if isDemoModel(model) {
		return spec, turbopg.InvalidInputf("embedding model %q is not supported", model)
	}
	if s.Embedder == nil {
		return spec, errEmbeddingsNotConfigured
	}
	vecs, err := s.Embedder.Embed(ctx, model, []string{spec.EmbedQuery}, embedDims)
	if err != nil {
		return spec, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return spec, fmt.Errorf("embedding provider returned an empty vector")
	}
	spec.Vector = vecs[0]
	return spec, nil
}

type openAIEmbedder struct {
	url    string
	apiKey string
	client *http.Client
}

func newOpenAIEmbedder(baseURL, apiKey string) *openAIEmbedder {
	return &openAIEmbedder{
		url:    embeddingsURL(baseURL),
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func embeddingsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/embeddings") {
		return base
	}
	return base + "/embeddings"
}

type openAIEmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (o *openAIEmbedder) Embed(ctx context.Context, model string, texts []string, dims int) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	reqBody := openAIEmbeddingRequest{Model: model, Input: texts, Dimensions: dims}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("embedding provider: invalid JSON: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("embedding provider: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 512 {
			msg = msg[:512]
		}
		return nil, fmt.Errorf("embedding provider: HTTP %d: %s", resp.StatusCode, msg)
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	out := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			continue
		}
		out[item.Index] = item.Embedding
	}
	for i, vec := range out {
		if len(vec) == 0 {
			return nil, fmt.Errorf("embedding provider: missing vector for input %d", i)
		}
	}
	return out, nil
}

func embedderFromConfig(baseURL, apiKey string) Embedder {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" && apiKey == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return newOpenAIEmbedder(baseURL, apiKey)
}
