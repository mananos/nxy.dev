package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// Decode reads one plan from r. An unknown field is an error that names it,
// and so is anything after the JSON object.
func Decode(r io.Reader) (domain.Plan, error) {
	var p domain.Plan
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return domain.Plan{}, fmt.Errorf("plan JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return domain.Plan{}, errors.New("plan JSON: extra content after the object")
	}
	return p, nil
}
