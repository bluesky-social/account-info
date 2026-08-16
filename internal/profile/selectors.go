package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/theory/jsonpath"
)

// ProfileSelectors maps canonical profile fields to RFC 9535 JSONPath
// expressions within a profile record. The selected fields may be absent from
// an individual record, but every selector-based source must declare singular
// expressions for their paths.
type ProfileSelectors struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Avatar      string `json:"avatar"`
	CreatedAt   string `json:"createdAt"`
}

type compiledProfileSelectors struct {
	displayName *jsonpath.Path
	description *jsonpath.Path
	avatar      *jsonpath.Path
	createdAt   *jsonpath.Path
}

func (s ProfileSelectors) configured() bool {
	return s != (ProfileSelectors{})
}

func (s ProfileSelectors) compile() (*compiledProfileSelectors, error) {
	displayName, err := compileProfileSelector("displayName", s.DisplayName)
	if err != nil {
		return nil, err
	}
	description, err := compileProfileSelector("description", s.Description)
	if err != nil {
		return nil, err
	}
	avatar, err := compileProfileSelector("avatar", s.Avatar)
	if err != nil {
		return nil, err
	}
	createdAt, err := compileProfileSelector("createdAt", s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &compiledProfileSelectors{
		displayName: displayName,
		description: description,
		avatar:      avatar,
		createdAt:   createdAt,
	}, nil
}

func compileProfileSelector(name, expression string) (*jsonpath.Path, error) {
	if expression == "" {
		return nil, fmt.Errorf("%s selector is empty", name)
	}
	path, err := jsonpath.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid %s selector: %w", name, err)
	}
	if path.Query().Singular() == nil {
		return nil, fmt.Errorf("%s selector must be singular", name)
	}
	return path, nil
}

func extractJSONProfile(
	account Identity,
	collection string,
	value json.RawMessage,
	selectors *compiledProfileSelectors,
) (Summary, error) {
	document, err := decodeJSONDocument(value)
	if err != nil {
		return Summary{}, fmt.Errorf("decode profile record: %w", err)
	}
	record, ok := document.(map[string]any)
	if !ok {
		return Summary{}, fmt.Errorf("decode profile record: root is not an object")
	}
	recordType, ok := record["$type"].(string)
	if !ok || recordType != collection {
		return Summary{}, fmt.Errorf(
			"record type does not match collection: got %q, want %q",
			recordType,
			collection,
		)
	}

	displayName, err := selectJSONString(document, selectors.displayName)
	if err != nil {
		return Summary{}, fmt.Errorf("extract displayName: %w", err)
	}
	description, err := selectJSONString(document, selectors.description)
	if err != nil {
		return Summary{}, fmt.Errorf("extract description: %w", err)
	}
	createdAt, err := selectJSONString(document, selectors.createdAt)
	if err != nil {
		return Summary{}, fmt.Errorf("extract createdAt: %w", err)
	}

	summary := Summary{
		DisplayName: displayName,
		Description: description,
		CreatedAt:   createdAt,
	}
	avatar, found, err := selectJSONValue(document, selectors.avatar)
	if err != nil {
		return Summary{}, fmt.Errorf("extract avatar: %w", err)
	}
	if !found || avatar == nil {
		return summary, nil
	}

	ref, err := decodeProfileBlob(avatar)
	if err != nil {
		return Summary{}, fmt.Errorf("decode avatar: %w", err)
	}
	if _, err := validateAvatarRef(ref); err != nil {
		return Summary{}, fmt.Errorf("decode avatar: %w", err)
	}
	avatarURL, err := blobURL(account.PDS, account.DID, ref.CID)
	if err != nil {
		return Summary{}, fmt.Errorf("build avatar URL: %w", err)
	}
	summary.Avatar = avatarURL
	summary.AvatarRef = &ref
	return summary, nil
}

func decodeJSONDocument(value json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return document, nil
}

func selectJSONString(document any, selector *jsonpath.Path) (string, error) {
	selected, found, err := selectJSONValue(document, selector)
	if err != nil || !found || selected == nil {
		return "", err
	}
	value, ok := selected.(string)
	if !ok {
		return "", fmt.Errorf("selected value is not a string")
	}
	return value, nil
}

func selectJSONValue(
	document any,
	selector *jsonpath.Path,
) (value any, found bool, err error) {
	selected := selector.Select(document)
	switch len(selected) {
	case 0:
		return nil, false, nil
	case 1:
		return selected[0], true, nil
	default:
		return nil, false, fmt.Errorf(
			"singular selector returned %d values",
			len(selected),
		)
	}
}

func decodeProfileBlob(value any) (BlobRef, error) {
	blob, ok := value.(map[string]any)
	if !ok {
		return BlobRef{}, fmt.Errorf("selected value is not an object")
	}
	blobType, err := requiredJSONString(blob, "$type")
	if err != nil {
		return BlobRef{}, err
	}
	if blobType != "blob" {
		return BlobRef{}, fmt.Errorf("invalid blob type %q", blobType)
	}
	refValue, ok := blob["ref"].(map[string]any)
	if !ok {
		return BlobRef{}, fmt.Errorf("blob ref is not an object")
	}
	cid, err := requiredJSONString(refValue, "$link")
	if err != nil {
		return BlobRef{}, fmt.Errorf("blob ref: %w", err)
	}
	contentType, err := requiredJSONString(blob, "mimeType")
	if err != nil {
		return BlobRef{}, err
	}
	sizeValue, ok := blob["size"].(json.Number)
	if !ok {
		return BlobRef{}, fmt.Errorf("blob size is not an integer")
	}
	size, err := sizeValue.Int64()
	if err != nil {
		return BlobRef{}, fmt.Errorf("blob size is not an integer: %w", err)
	}
	return BlobRef{CID: cid, ContentType: contentType, Size: size}, nil
}

func requiredJSONString(object map[string]any, name string) (string, error) {
	value, ok := object[name]
	if !ok {
		return "", fmt.Errorf("%s is missing", name)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", name)
	}
	return text, nil
}
