package domain

// LoginIdentifier carries exactly one of phone or email as a login identity.
// Phone holds the validated E.164 form, email the validated address; the
// other field stays empty.
type LoginIdentifier struct {
	Phone string
	Email string
}

// NewLoginIdentifier validates that exactly one identifier form is present
// and well-formed.
func NewLoginIdentifier(phone, email string) (LoginIdentifier, error) {
	if (phone != "") == (email != "") {
		return LoginIdentifier{}, ErrIdentifierInvalid
	}
	if phone != "" {
		p, err := NewPhone(phone)
		if err != nil {
			return LoginIdentifier{}, err
		}
		return LoginIdentifier{Phone: p.String()}, nil
	}
	e, err := NewEmail(email)
	if err != nil {
		return LoginIdentifier{}, err
	}
	return LoginIdentifier{Email: e.String()}, nil
}

// Value returns the single non-empty identifier string.
func (i LoginIdentifier) Value() string {
	if i.Phone != "" {
		return i.Phone
	}
	return i.Email
}

// Channel returns the delivery channel for this identifier's verification
// codes.
func (i LoginIdentifier) Channel() string {
	if i.Phone != "" {
		return "sms"
	}
	return "email"
}
