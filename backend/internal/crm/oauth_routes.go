package crm

import (
	"net/http"
	"net/url"

	"cardflow-backend/internal/crm/oauth"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
)

// oauthCallback finishes "Sign in with Google / Microsoft / LinkedIn" and "Connect
// Gmail / Outlook" (D-64): the signed state says which, and the nonce cookie proves the
// flow started in this browser.
func (m *Module) oauthCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(path, msg string) {
		if path == "" {
			path = "/crm/login"
		}
		http.Redirect(w, r, path+"?oauthError="+url.QueryEscape(msg), http.StatusFound)
	}
	q := r.URL.Query()
	st, err := oauth.DecodeState(m.cfg.EncryptionKey, q.Get("state"))
	if err != nil || st.Provider != chi.URLParam(r, "provider") {
		fail("", "That sign-in link expired. Try again.")
		return
	}
	c, err := r.Cookie("crm_oauth")
	if err != nil || !shared.ConstantTimeEqual(c.Value, st.Nonce) {
		fail(st.Return, "Finish signing in in the same browser you started in.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "crm_oauth", Value: "", Path: "/api/crm/v1/oauth", MaxAge: -1})
	if e := q.Get("error"); e != "" {
		fail(st.Return, "Sign-in was cancelled.")
		return
	}
	switch st.Purpose {
	case "mailbox":
		next, err := m.records.CompleteMailboxConnect(r.Context(), st, q.Get("code"))
		if err != nil {
			fail(st.Return, err.Error())
			return
		}
		http.Redirect(w, r, next, http.StatusFound)
	case "login":
		next, err := m.identity.CompleteOAuthLogin(w, r, st, q.Get("code"))
		if err != nil {
			fail(st.Return, errText(err))
			return
		}
		http.Redirect(w, r, next, http.StatusFound)
	default:
		fail("", "Unknown sign-in.")
	}
}

func errText(err error) string {
	if e, ok := err.(*shared.Error); ok {
		return e.Message
	}
	return err.Error()
}

// oauthProviders tells the sign-in page which buttons can work on this server.
func (m *Module) oauthProviders(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, http.StatusOK, oauth.Configured(m.cfg.BaseURL))
}
