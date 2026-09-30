package contract

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove/driver"
)

// maxPreviewBytes caps a preview at 256 KiB, the most the page shows.
const maxPreviewBytes = 256 * 1024

type contentURLInput struct {
	Store   string `json:"store"`
	Bucket  string `json:"bucket"`
	Key     string `json:"key"`
	Purpose string `json:"purpose"`
	Limit   int64  `json:"limit"`
}

func subjectOf(p contract.Principal) string {
	if p.User == nil {
		return ""
	}
	return strings.TrimSpace(p.User.Subject)
}

func objectsContentURLHandler(deps Deps) func(context.Context, contentURLInput, contract.Principal) (linkOutput, error) {
	return func(ctx context.Context, in contentURLInput, p contract.Principal) (linkOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return linkOutput{}, err
		}
		if in.Key == "" {
			return linkOutput{}, badRequest("key is required")
		}
		op := OpDownload
		switch in.Purpose {
		case "", "download":
		case "preview":
			op = OpPreview
		default:
			return linkOutput{}, badRequest(`purpose must be "download" or "preview"`)
		}
		if in.Limit < 0 {
			return linkOutput{}, badRequest("limit cannot be negative")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return linkOutput{}, err
		}
		if _, err = st.Trove.Head(ctx, in.Bucket, in.Key); err != nil {
			return linkOutput{}, deps.mapError("objects.contentUrl", err)
		}
		tk := Ticket{Store: st.Name, Bucket: in.Bucket, Key: in.Key, Op: op, Subject: subjectOf(p)}
		if op == OpPreview {
			tk.Limit = in.Limit
			if tk.Limit == 0 || tk.Limit > maxPreviewBytes {
				tk.Limit = maxPreviewBytes
			}
		}
		tok, expires, err := deps.Content.Signer.Issue(tk, DownloadTicketTTL)
		if err != nil {
			return linkOutput{}, deps.mapError("objects.contentUrl", err)
		}
		return linkOutput{URL: deps.Content.URL(tok), ExpiresAt: expires.UTC().Format(time.RFC3339)}, nil
	}
}

type beginUploadInput struct {
	Store       string `json:"store"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	Overwrite   bool   `json:"overwrite"`
}

func objectsBeginUploadHandler(deps Deps) func(context.Context, beginUploadInput, contract.Principal) (linkOutput, error) {
	return func(ctx context.Context, in beginUploadInput, p contract.Principal) (linkOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return linkOutput{}, err
		}
		if in.Key == "" {
			return linkOutput{}, badRequest("key is required")
		}
		if in.Size < 0 {
			return linkOutput{}, badRequest("size cannot be negative")
		}
		if in.Size > deps.Content.MaxUploadBytes {
			return linkOutput{}, &contract.Error{
				Code:    contract.CodeBadRequest,
				Message: fmt.Sprintf("This file is larger than the upload limit of %d bytes.", deps.Content.MaxUploadBytes),
				Details: map[string]any{"maxUploadBytes": deps.Content.MaxUploadBytes},
			}
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return linkOutput{}, err
		}
		err = refuseCASBucket(st.Trove, in.Bucket)
		if err != nil {
			return linkOutput{}, err
		}
		if !in.Overwrite {
			_, err = st.Trove.Head(ctx, in.Bucket, in.Key)
			if err == nil {
				return linkOutput{}, &contract.Error{
					Code: contract.CodeConflict, Message: "an object with this key already exists",
					Details: map[string]any{"exists": true},
				}
			}
			if !errors.Is(err, driver.ErrObjectNotFound) {
				return linkOutput{}, deps.mapError("objects.beginUpload", err)
			}
		}
		tk := Ticket{
			Store: st.Name, Bucket: in.Bucket, Key: in.Key, Op: OpUpload, Subject: subjectOf(p),
			Size: in.Size, ContentType: in.ContentType, Overwrite: in.Overwrite,
		}
		tok, expires, err := deps.Content.Signer.Issue(tk, UploadTicketTTL)
		if err != nil {
			return linkOutput{}, deps.mapError("objects.beginUpload", err)
		}
		return linkOutput{URL: deps.Content.URL(tok), ExpiresAt: expires.UTC().Format(time.RFC3339)}, nil
	}
}

// objectsCompleteUploadHandler confirms what the content route stored. It
// exists so the upload ends in a command, which is what carries
// meta.invalidates to the client: the React shell refreshes through that
// and nothing else.
func objectsCompleteUploadHandler(deps Deps) func(context.Context, objectKeyInput, contract.Principal) (objectRow, error) {
	return func(ctx context.Context, in objectKeyInput, _ contract.Principal) (objectRow, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return objectRow{}, err
		}
		if in.Key == "" {
			return objectRow{}, badRequest("key is required")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return objectRow{}, err
		}
		info, err := st.Trove.Head(ctx, in.Bucket, in.Key)
		if err != nil {
			return objectRow{}, deps.mapError("objects.completeUpload", err)
		}
		return projectObjectRow(*info), nil
	}
}
