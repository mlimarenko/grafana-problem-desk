package plugin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func (a *App) handleGetAssignees(ctx context.Context, orgID int64, sender backend.CallResourceResponseSender) error {
	c, err := a.store.GetAssignees(ctx, orgID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to load assignees"})
	}
	return sendJSON(sender, http.StatusOK, c)
}

func validAssigneeText(s string, required bool) bool {
	if !utf8.ValidString(s) || len(s) > 255 || (required && strings.TrimSpace(s) == "") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validAssigneeJSONUnicode prevents encoding/json from replacing malformed Unicode.
// JSON syntax and object validation remain the strict decoder's responsibility.
func validAssigneeJSONUnicode(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	hex4 := func(b []byte) (uint16, bool) {
		if len(b) < 4 {
			return 0, false
		}
		var value uint16
		for _, c := range b[:4] {
			value <<= 4
			switch {
			case c >= '0' && c <= '9':
				value += uint16(c - '0')
			case c >= 'a' && c <= 'f':
				value += uint16(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				value += uint16(c - 'A' + 10)
			default:
				return 0, false
			}
		}
		return value, true
	}
	inString := false
	for i := 0; i < len(body); i++ {
		if body[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || body[i] != '\\' || i+1 >= len(body) {
			continue
		}
		i++
		if body[i] != 'u' {
			continue
		}
		value, ok := hex4(body[i+1:])
		if !ok {
			continue
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
				return false
			}
			low, ok := hex4(body[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func (a *App) handlePutAssignees(ctx context.Context, orgID int64, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}
	var p struct {
		OrgID        *int64 `json:"orgId"`
		BaseRevision *int64 `json:"baseRevision"`
		Members      *[]struct {
			UID   *string `json:"uid"`
			Login *string `json:"login"`
			Name  *string `json:"name"`
		} `json:"members"`
	}
	if !validAssigneeJSONUnicode(req.Body) {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid catalog"})
	}
	if err := decodeJSONStrict(req.Body, &p); err != nil || p.OrgID == nil || p.BaseRevision == nil || p.Members == nil || *p.BaseRevision < 0 || len(*p.Members) > 5000 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid catalog"})
	}
	if *p.OrgID != orgID {
		return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "organization mismatch"})
	}
	members := make([]models.AssigneeMember, 0, len(*p.Members))
	uids, logins := map[string]bool{}, map[string]bool{}
	for _, m := range *p.Members {
		if m.UID == nil || m.Login == nil || m.Name == nil || !validAssigneeText(*m.UID, true) || !validAssigneeText(*m.Login, true) || !validAssigneeText(*m.Name, false) || uids[*m.UID] || logins[*m.Login] {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid catalog member"})
		}
		uids[*m.UID], logins[*m.Login] = true, true
		members = append(members, models.AssigneeMember{UID: *m.UID, Login: *m.Login, Name: *m.Name})
	}
	c, err := a.store.PutAssignees(ctx, orgID, actor, *p.BaseRevision, members)
	if errors.Is(err, storage.ErrAssigneeStale) {
		return sendJSON(sender, http.StatusConflict, map[string]string{"error": "stale_revision"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to save assignees"})
	}
	return sendJSON(sender, http.StatusOK, c)
}
