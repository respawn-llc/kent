package speech

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"

	"core/server/llm"
)

// transcribe owns both sides of the multipart pipe. Closing input interrupts a
// producer blocked waiting for the next microphone chunk, including early
// provider rejection.
func transcribe(ctx context.Context, invocation Invocation, credential *llm.DispatchAuth, encoding llm.SpeechEncoding, input io.ReadCloser) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, invocation.Profile.Endpoint, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return "", err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	if credential != nil {
		request.Header.Set("Authorization", credential.Header)
		if credential.Mode.AccountID != "" {
			request.Header.Set("ChatGPT-Account-Id", credential.Mode.AccountID)
		}
	}
	done := make(chan error, 1)
	stopInput := context.AfterFunc(ctx, func() {
		_ = reader.CloseWithError(ctx.Err())
		_ = input.Close()
	})
	defer stopInput()
	go func() {
		err := writeMultipart(multipartWriter, invocation.Profile, encoding, input)
		_ = writer.CloseWithError(err)
		done <- err
	}()
	response, requestErr := llm.NewProviderHTTPClient(invocation.Profile.Endpoint, 0).Do(request)
	if requestErr != nil {
		cancel()
		_ = reader.CloseWithError(requestErr)
		closeErr := input.Close()
		producerErr := <-done
		return "", errors.Join(requestErr, producerErr, closeErr)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = reader.CloseWithError(io.ErrClosedPipe)
		closeErr := input.Close()
		producerErr := <-done
		variant, resolveErr := llm.ResolveConnectionVariant(invocation.Definition)
		if resolveErr != nil {
			return "", errors.Join(resolveErr, producerErr, closeErr)
		}
		// Reuse the provider's diagnostic decoder, bounding only the JSON/error
		// response; audio is never buffered here.
		response.Body = &boundedResponse{Reader: io.LimitReader(response.Body, 1<<20), Closer: response.Body}
		providerErr, _ := variant.Variant.NewErrorReducer(variant.Variant.ProviderID).Reduce(nil, response)
		return "", errors.Join(providerErr, producerErr, closeErr)
	}
	var payload struct {
		Text *string `json:"text"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload)
	if decodeErr == nil && payload.Text == nil {
		decodeErr = errors.New("transcription response is missing text")
	}
	// A provider response terminates this upload even when it arrives before
	// ingress finishes. Never leave the producer waiting on its input.
	_ = reader.CloseWithError(io.ErrClosedPipe)
	closeErr := input.Close()
	producerErr := <-done
	if err := errors.Join(decodeErr, producerErr, closeErr); err != nil {
		return "", err
	}
	return *payload.Text, nil
}

type boundedResponse struct {
	io.Reader
	io.Closer
}

func writeMultipart(writer *multipart.Writer, profile llm.SpeechProfile, encoding llm.SpeechEncoding, input io.Reader) error {
	if profile.Model != nil {
		if err := writer.WriteField("model", *profile.Model); err != nil {
			return err
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name": "file", "filename": "recording." + encoding.Extension,
	}))
	header.Set("Content-Type", encoding.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, input); err != nil {
		return fmt.Errorf("read speech upload: %w", err)
	}
	return writer.Close()
}
