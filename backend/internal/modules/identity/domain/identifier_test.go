package domain

import "testing"

func TestNewLoginIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		phone   string
		email   string
		want    LoginIdentifier
		wantErr error
	}{
		{"phone only", "+8613800138000", "", LoginIdentifier{Phone: "+8613800138000"}, nil},
		{"email only", "", "admin@example.com", LoginIdentifier{Email: "admin@example.com"}, nil},
		{"email mixed case normalizes", "", "Admin@Example.COM", LoginIdentifier{Email: "admin@example.com"}, nil},
		{"both present", "+8613800138000", "admin@example.com", LoginIdentifier{}, ErrIdentifierInvalid},
		{"neither present", "", "", LoginIdentifier{}, ErrIdentifierInvalid},
		{"invalid phone", "abc", "", LoginIdentifier{}, ErrInvalidPhone},
		{"invalid email", "", "not-an-email", LoginIdentifier{}, ErrInvalidEmail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewLoginIdentifier(tc.phone, tc.email)
			if err != tc.wantErr {
				t.Fatalf("NewLoginIdentifier(%q, %q) error = %v, want %v", tc.phone, tc.email, err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Fatalf("NewLoginIdentifier(%q, %q) = %#v, want %#v", tc.phone, tc.email, got, tc.want)
			}
		})
	}
}

func TestLoginIdentifierValueAndChannel(t *testing.T) {
	phone := LoginIdentifier{Phone: "+8613800138000"}
	if phone.Value() != "+8613800138000" || phone.Channel() != "sms" {
		t.Fatalf("phone identifier = %q/%q", phone.Value(), phone.Channel())
	}
	email := LoginIdentifier{Email: "admin@example.com"}
	if email.Value() != "admin@example.com" || email.Channel() != "email" {
		t.Fatalf("email identifier = %q/%q", email.Value(), email.Channel())
	}
}
