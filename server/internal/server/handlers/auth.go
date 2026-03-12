package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/kp-cms/server/internal/auth"
)

func HandleLogin(authSvc *auth.AuthService, auditLogger interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest

		if err := decodeJSON(w, r, &req); err != nil {
			WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
			return
		}

		session, err := authSvc.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			WriteError(w, r, http.StatusUnauthorized, ErrCodeInvalidCredentials, ErrMsgInvalidCreds)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    session.Token,
			Path:     "/",
			HttpOnly: true,
			Expires:  session.ExpiresAt,
		})

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token":   session.Token,
			"expires": session.ExpiresAt.Format(time.RFC3339),
		})
	}
}

func HandleLogout(authSvc *auth.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "" {
			token = strings.TrimPrefix(token, BearerPrefix)
			authSvc.Logout(r.Context(), token)
		}

		if _, err := r.Cookie("session"); err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:   "session",
				Value:  "",
				Path:   "/",
				MaxAge: -1,
			})
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "logged out",
		})
	}
}
