package opencodeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

const maxCatalogBody = 8 << 20

// Model is the bounded public representation returned by the model catalog.
type Model struct {
	ID      string
	Object  string
	Created int64
	OwnedBy string
}

// ListModels fetches the public model catalog. The endpoint does not use the
// configured API key or conversation session.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	if c == nil {
		return nil, ErrInvalidConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.endpoint("/models"), nil)
	if err != nil {
		return nil, errCatalogRequest
	}
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, catalogRequestError(ctx, err)
	}
	if resp == nil {
		return nil, errCatalogRequest
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, DecodeHTTPError(resp)
	}
	if resp.Body == nil {
		return nil, errCatalogRead
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBody+1))
	if err != nil {
		if contextErr := contextError(ctx, err); contextErr != nil {
			return nil, contextErr
		}
		return nil, errCatalogRead
	}
	if len(body) > maxCatalogBody {
		return nil, errCatalogDecode
	}
	models, err := decodeModels(body)
	if err != nil {
		return nil, errCatalogDecode
	}
	return models, nil
}

func catalogRequestError(ctx context.Context, err error) error {
	if contextErr := contextError(ctx, err); contextErr != nil {
		return contextErr
	}
	return errCatalogRequest
}

func contextError(ctx context.Context, err error) error {
	if ctx != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func decodeModels(body []byte) ([]Model, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, errCatalogDecode
	}

	var object string
	if err := decodeRequiredString(envelope, "object", &object); err != nil || object != "list" {
		return nil, errCatalogDecode
	}
	data, ok := envelope["data"]
	if !ok || string(data) == "null" {
		return nil, errCatalogDecode
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil || entries == nil && string(data) != "[]" {
		return nil, errCatalogDecode
	}
	models := make([]Model, 0, len(entries))
	for _, entry := range entries {
		model, err := decodeModel(entry)
		if err != nil {
			return nil, errCatalogDecode
		}
		models = append(models, model)
	}
	return models, nil
}

func decodeModel(body []byte) (Model, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return Model{}, errCatalogDecode
	}
	var model Model
	if err := decodeRequiredString(fields, "id", &model.ID); err != nil || model.ID == "" {
		return Model{}, errCatalogDecode
	}
	if err := decodeOptionalString(fields, "object", &model.Object); err != nil {
		return Model{}, errCatalogDecode
	}
	if raw, ok := fields["created"]; ok {
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err != nil {
			return Model{}, errCatalogDecode
		}
		created, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return Model{}, errCatalogDecode
		}
		model.Created = created
	}
	if err := decodeOptionalString(fields, "owned_by", &model.OwnedBy); err != nil {
		return Model{}, errCatalogDecode
	}
	return model, nil
}

func decodeRequiredString(fields map[string]json.RawMessage, name string, target *string) error {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return errCatalogDecode
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return errCatalogDecode
	}
	return nil
}

func decodeOptionalString(fields map[string]json.RawMessage, name string, target *string) error {
	raw, ok := fields[name]
	if !ok {
		return nil
	}
	if string(raw) == "null" {
		return errCatalogDecode
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return errCatalogDecode
	}
	return nil
}
