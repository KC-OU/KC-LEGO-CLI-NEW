package uiapp

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestFeatureRequestLogsAnAdminEventAndNotifies covers the generalized
// request flow (a theme, a feature, a fix — anything), which replaced the
// old theme-only one; the theme picker's 'R' still routes here (see
// theme_picker_test.go).
func TestFeatureRequestLogsAnAdminEventAndNotifies(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrFeatureRequest)
	app.screens[scrFeatureRequest].(*formScreen).submit(app, []string{"Solarized theme", "https://ethanschoonover.com/solarized/"})

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Kind != lego.EventFeatureRequest {
		t.Fatalf("AdminEvents = %+v, %v, want one feature_request event", evs, err)
	}
	if !strings.Contains(evs[0].Detail, "Solarized theme") || !strings.Contains(evs[0].Detail, "ethanschoonover") {
		t.Errorf("detail = %q, want what was asked for and the link", evs[0].Detail)
	}
	if evs[0].Actor != "admin" {
		t.Errorf("actor = %q, want the signed-in user", evs[0].Actor)
	}
	if !auditHas(t, "FEATURE_REQUESTED") {
		t.Error("expected an audit log entry")
	}
}

func TestFeatureRequestRequiresSomething(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrFeatureRequest)
	app.screens[scrFeatureRequest].(*formScreen).submit(app, []string{"", "https://example.com"})
	if !app.messageErr {
		t.Error("a blank request should be refused")
	}
	if evs, _ := app.legoDB.AdminEvents(10); len(evs) != 0 {
		t.Errorf("nothing should be logged on refusal, got %+v", evs)
	}
}

// TestMessageAdminLogsAnAdminEventAndNotifies covers the reverse of the
// admin's existing "Message a User": no single admin account to deliver to
// (admin is a role), so this lands in Recent Activity plus a notification
// instead of a per-recipient inbox message.
func TestMessageAdminLogsAnAdminEventAndNotifies(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrMessageAdmin)
	app.screens[scrMessageAdmin].(*formScreen).submit(app, []string{"running about 20 minutes behind on the morning sets"})

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Kind != lego.EventMessage {
		t.Fatalf("AdminEvents = %+v, %v, want one message event", evs, err)
	}
	if evs[0].Target != "" {
		t.Errorf("target = %q, want empty — this has no single recipient", evs[0].Target)
	}
	if !strings.Contains(evs[0].Detail, "running about 20 minutes behind") {
		t.Errorf("detail = %q, want the message body", evs[0].Detail)
	}
	if !auditHas(t, "MESSAGE_TO_ADMIN") {
		t.Error("expected an audit log entry")
	}
}

func TestMessageAdminRequiresABody(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrMessageAdmin)
	app.screens[scrMessageAdmin].(*formScreen).submit(app, []string{""})
	if !app.messageErr {
		t.Error("a blank message should be refused")
	}
	if evs, _ := app.legoDB.AdminEvents(10); len(evs) != 0 {
		t.Errorf("nothing should be logged on refusal, got %+v", evs)
	}
}
