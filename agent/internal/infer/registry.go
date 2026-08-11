package infer

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed templates/*.json
var templatesFS embed.FS

// Registry holds loaded templates indexed by ID.
type Registry struct {
	mu        sync.RWMutex
	templates map[string]*Template
}

// NewRegistry loads all templates from the embedded filesystem.
func NewRegistry() (*Registry, error) {
	r := &Registry{
		templates: make(map[string]*Template),
	}

	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := templatesFS.ReadFile("templates/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", entry.Name(), err)
		}
		var t Template
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse template %s: %w", entry.Name(), err)
		}
		r.templates[t.ID] = &t
	}

	return r, nil
}

// Get returns a template by ID, or nil if not found.
func (r *Registry) Get(id string) *Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[id]
}

// List returns all loaded templates.
func (r *Registry) List() []*Template {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Template, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	return result
}

// SelectQuantization picks the best quantization for the available RAM.
// Returns the quant key and its QuantInfo.
func (t *Template) SelectQuantization(availableRAMGB float64) (string, *QuantInfo) {
	// Try default first
	if q, ok := t.Model.Quantizations[t.Model.DefaultQuant]; ok {
		if availableRAMGB >= q.MinRAMGB {
			return t.Model.DefaultQuant, &q
		}
	}
	// Try fallbacks in order
	for _, quant := range t.Model.FallbackQuants {
		if q, ok := t.Model.Quantizations[quant]; ok {
			if availableRAMGB >= q.MinRAMGB {
				return quant, &q
			}
		}
	}
	// Return default even if RAM is insufficient (caller will block)
	if q, ok := t.Model.Quantizations[t.Model.DefaultQuant]; ok {
		return t.Model.DefaultQuant, &q
	}
	return "", nil
}

// ModelFileName returns the GGUF filename for the given quantization.
func (t *Template) ModelFileName(quant string) string {
	if q, ok := t.Model.Quantizations[quant]; ok {
		return q.FileName
	}
	return ""
}
