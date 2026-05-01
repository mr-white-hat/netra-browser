package browser

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

var (
	ErrDownloadCanceled = errors.New("download canceled")
	ErrDownloadTimeout  = errors.New("download timeout")
)

type DownloadResult struct {
	FilePath string `json:"file_path"`
	Size     int64  `json:"size"`
}

// WaitForDownload sets download behavior, runs trigger (if non-nil), and waits for completion.
// saveTo is the directory Chrome should drop files into. Files land at <saveTo>/<guid>.
func (p *Page) WaitForDownload(ctx context.Context, trigger func() error, saveTo string, timeout time.Duration) (*DownloadResult, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if _, err := p.send(ctx, "Browser.setDownloadBehavior", map[string]any{
		"behavior":     "allow",
		"downloadPath": saveTo,
	}); err != nil {
		return nil, err
	}

	willBegin := p.cdp.SubscribeOnTarget(p.sessionID, "Browser.downloadWillBegin")
	progress := p.cdp.SubscribeOnTarget(p.sessionID, "Browser.downloadProgress")

	if trigger != nil {
		if err := trigger(); err != nil {
			return nil, err
		}
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var guid string

	for {
		select {
		case e := <-willBegin:
			var v struct {
				GUID string `json:"guid"`
			}
			_ = json.Unmarshal(e.Params, &v)
			if v.GUID != "" {
				guid = v.GUID
			}
		case e := <-progress:
			var v struct {
				GUID          string `json:"guid"`
				ReceivedBytes int64  `json:"receivedBytes"`
				State         string `json:"state"`
			}
			_ = json.Unmarshal(e.Params, &v)
			if v.State == "completed" {
				if guid == "" {
					guid = v.GUID
				}
				return &DownloadResult{
					FilePath: filepath.Join(saveTo, guid),
					Size:     v.ReceivedBytes,
				}, nil
			}
			if v.State == "canceled" {
				return nil, ErrDownloadCanceled
			}
		case <-deadline.C:
			return nil, ErrDownloadTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
