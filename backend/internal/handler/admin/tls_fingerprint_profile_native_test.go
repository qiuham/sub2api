package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type nativeProfileRepo struct {
	rows []*model.TLSFingerprintProfile
}

func (r *nativeProfileRepo) List(context.Context) ([]*model.TLSFingerprintProfile, error) {
	return r.rows, nil
}
func (r *nativeProfileRepo) GetByID(_ context.Context, id int64) (*model.TLSFingerprintProfile, error) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return nil, nil
}
func (r *nativeProfileRepo) Create(_ context.Context, row *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	row.ID = int64(len(r.rows) + 1)
	r.rows = append(r.rows, row)
	return row, nil
}
func (r *nativeProfileRepo) Update(_ context.Context, row *model.TLSFingerprintProfile) (*model.TLSFingerprintProfile, error) {
	return row, nil
}
func (r *nativeProfileRepo) Delete(context.Context, int64) error { return nil }

func TestCreateNativeProfileRouteIsIdempotentAndMatchesFamily(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &nativeProfileRepo{}
	h := NewTLSFingerprintProfileHandler(service.NewTLSFingerprintProfileService(repo, nil))
	router := gin.New()
	router.POST("/native/:family", h.CreateNative)
	for _, family := range []string{"claude", "claude", "codex"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/native/"+family, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", family, w.Code, w.Body.String())
		}
		var body struct {
			Code int `json:"code"`
			Data struct {
				model.TLSFingerprintProfile
				NativeFamily   string   `json:"native_family"`
				NativeVersions []string `json:"native_versions"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Code != 0 {
			t.Fatalf("%s response=%s", family, w.Body.String())
		}
		version := map[string]string{"claude": "2.1.281", "codex": "0.156.1"}[family]
		if !tlsfingerprint.MatchesVerifiedNativeProfile(body.Data.ToTLSProfile(), tlsfingerprint.VerifiedNativeProfile(family, version)) {
			t.Fatalf("%s installed an unrelated template", family)
		}
		if body.Data.NativeFamily != family || len(body.Data.NativeVersions) != 3 {
			t.Fatalf("%s missing verified version metadata: %+v", family, body.Data)
		}
	}
	if len(repo.rows) != 2 {
		t.Fatalf("repeated install created %d templates", len(repo.rows))
	}
}
