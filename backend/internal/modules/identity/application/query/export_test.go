package query

import (
	"context"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func TestGetChannelPreferencesReturnsOnlyPreferenceFields(t *testing.T) {
	expires := testNow.Add(time.Hour)
	store := &fakeChannelStore{channels: []domain.ContactChannel{
		{ID: "c1", UserID: "u1", WorkspaceID: "w1", Kind: domain.ChannelKindEmail, Address: "a@example.com", Verified: true, Enabled: true, CodeHash: domain.HashCode("222333"), CodeExpiresAt: &expires, CreatedAt: testNow},
		{ID: "c2", UserID: "u1", WorkspaceID: "w1", Kind: domain.ChannelKindSMS, Address: "+8613800137001", Verified: false, Enabled: false, CreatedAt: testNow},
		{ID: "c3", UserID: "u2", WorkspaceID: "w1", Kind: domain.ChannelKindEmail, Address: "other@example.com", Verified: true, Enabled: true, CreatedAt: testNow},
	}}
	q := &ChannelsExportQuery{Channels: store}

	prefs, err := q.GetChannelPreferences(context.Background(), dto.Principal{UserID: "u1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("GetChannelPreferences() error = %v", err)
	}
	want := []dto.ChannelPreference{
		{ID: "c1", Kind: "email", Address: "a@example.com", Enabled: true},
		{ID: "c2", Kind: "sms", Address: "+8613800137001", Enabled: false},
	}
	if len(prefs) != len(want) {
		t.Fatalf("prefs = %#v, want %v", prefs, want)
	}
	for i := range want {
		if prefs[i] != want[i] {
			t.Fatalf("prefs[%d] = %#v, want %#v", i, prefs[i], want[i])
		}
	}
}

func TestGetChannelPreferencesEmptyList(t *testing.T) {
	store := &fakeChannelStore{}
	q := &ChannelsExportQuery{Channels: store}

	prefs, err := q.GetChannelPreferences(context.Background(), dto.Principal{UserID: "u1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("GetChannelPreferences() error = %v", err)
	}
	if prefs == nil || len(prefs) != 0 {
		t.Fatalf("prefs = %#v, want empty non-nil list", prefs)
	}
}
