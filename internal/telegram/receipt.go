package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	tgbot "github.com/go-telegram/bot"
)

const maxReceiptBytes = 20 << 20

// Only the wait for response headers is bounded; the body stream is governed by
// the request context, so a slow connection does not truncate a large file.
var receiptClient = &http.Client{Transport: func() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return t
}()}

// scrubURL drops the URL from a *url.Error: the download URL carries the bot
// token and must not reach logs. (bot.GetFile already masks the token itself.)
func scrubURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// Receipt opens the file a customer sent as a receipt. The download URL carries
// the bot token, so the bytes are streamed through the server and the URL never
// leaves it. The caller closes the reader.
func (b *Bot) Receipt(ctx context.Context, fileID string) (io.ReadCloser, string, error) {
	f, err := b.tg.GetFile(ctx, &tgbot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, "", err
	}
	if f.FilePath == "" {
		return nil, "", errors.New("receipt: telegram returned no file path")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.tg.FileDownloadLink(f), nil)
	if err != nil {
		return nil, "", fmt.Errorf("receipt: build request: %w", scrubURL(err))
	}
	resp, err := receiptClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("receipt: %w", scrubURL(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("receipt: telegram answered HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxReceiptBytes {
		resp.Body.Close()
		return nil, "", fmt.Errorf("receipt: file is %d bytes, over the %d limit", resp.ContentLength, maxReceiptBytes)
	}
	body := struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, maxReceiptBytes), resp.Body}
	return body, resp.Header.Get("Content-Type"), nil
}
