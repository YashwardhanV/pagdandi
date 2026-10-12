package handlers

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"pagdandi/api/internal/auth"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/httpx"
	"pagdandi/api/internal/shortcuts"
	"pagdandi/api/internal/store"
)

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": u, "trusted": u.Trust >= 1.5})
	return nil
}

type patchMeBody struct {
	DisplayName   *string `json:"display_name"`
	HomeRegion    *string `json:"home_region"`
	TracesConsent *bool   `json:"traces_consent"`
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	var b patchMeBody
	if err := httpx.DecodeJSON(w, r, &b, 4<<10); err != nil {
		return err
	}
	if b.DisplayName != nil {
		n := strings.TrimSpace(*b.DisplayName)
		if n == "" || len([]rune(n)) > 40 {
			return badParam("display_name", "1 to 40 characters")
		}
		b.DisplayName = &n
	}
	if b.HomeRegion != nil {
		if _, ok := s.Regions.Get(*b.HomeRegion); !ok {
			return badParam("home_region", "unknown region")
		}
	}
	out, err := s.DB.UpdateUser(r.Context(), u.ID, b.DisplayName, b.HomeRegion, b.TracesConsent)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": out, "trusted": out.Trust >= 1.5})
	return nil
}

func (s *Server) deleteMe(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteUser(r.Context(), u.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) contributions(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	list, reports, err := s.DB.UserContributions(r.Context(), u.ID)
	if err != nil {
		return err
	}
	if list == nil {
		list = []store.Shortcut{}
	}
	active := 0
	for _, sc := range list {
		if sc.Status == shortcuts.StatusActive {
			active++
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"shortcuts": list, "reports": reports, "active_shortcuts": active})
	return nil
}

func (s *Server) places(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	ps, err := s.DB.Places(r.Context(), u.ID)
	if err != nil {
		return err
	}
	if ps == nil {
		ps = []store.Place{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"places": ps})
	return nil
}

func placeKind(r *http.Request) (string, error) {
	k := r.PathValue("kind")
	if !slices.Contains([]string{"home", "work"}, k) {
		return "", httpx.Err(http.StatusNotFound, "NOT_FOUND", "Place must be home or work.")
	}
	return k, nil
}

func (s *Server) putPlace(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	k, err := placeKind(r)
	if err != nil {
		return err
	}
	var p geo.Point
	if err := httpx.DecodeJSON(w, r, &p, 1<<10); err != nil {
		return err
	}
	if !p.Valid() {
		return badParam("lat/lng", "out of range")
	}
	if err := s.DB.SetPlace(r.Context(), u.ID, k, &p); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) deletePlace(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	k, err := placeKind(r)
	if err != nil {
		return err
	}
	if err := s.DB.SetPlace(r.Context(), u.ID, k, nil); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) adminQueue(w http.ResponseWriter, r *http.Request) error {
	if _, err := auth.RequireRole(r, "moderator", "admin"); err != nil {
		return err
	}
	items, err := s.DB.AdminQueue(r.Context(), 100)
	if err != nil {
		return err
	}
	if items == nil {
		items = []store.AdminQueueItem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type adminActionBody struct {
	Action string   `json:"action"` // approve | remove | edit_flags | dismiss
	Flags  []string `json:"flags"`
	Note   string   `json:"note"`
}

func (s *Server) adminAction(w http.ResponseWriter, r *http.Request) error {
	mod, err := auth.RequireRole(r, "moderator", "admin")
	if err != nil {
		return err
	}
	var b adminActionBody
	if err := httpx.DecodeJSON(w, r, &b, 4<<10); err != nil {
		return err
	}
	id := r.PathValue("id")
	sc, err := s.DB.GetShortcut(r.Context(), id)
	if err != nil {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Shortcut not found.")
	}
	switch b.Action {
	case "approve":
		if err := s.DB.SetShortcutStatus(r.Context(), sc.ID, shortcuts.StatusActive); err != nil {
			return err
		}
	case "remove":
		if err := s.DB.SetShortcutStatus(r.Context(), sc.ID, shortcuts.StatusRemoved); err != nil {
			return err
		}
	case "edit_flags":
		flags, err := cleanList(b.Flags, allowedFlags, "flags")
		if err != nil {
			return err
		}
		if _, err := s.DB.UpdateShortcutMeta(r.Context(), sc.ID, nil, flags, nil, nil); err != nil {
			return err
		}
	case "dismiss":
		// Report reviewed, no change to the shortcut.
	default:
		return badParam("action", "must be approve, remove, edit_flags or dismiss")
	}
	if len(b.Note) > 500 {
		b.Note = b.Note[:500]
	}
	if err := s.DB.LogModeration(r.Context(), sc.ID, mod.ID, b.Action, b.Note); err != nil {
		return err
	}
	out, err := s.DB.GetShortcut(r.Context(), sc.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"shortcut": out})
	return nil
}
