package model

// CheckinInputError contains a fixed, public validation message. Database and
// network errors must never be returned verbatim by the check-in API.
type CheckinInputError struct{ Message string }

func (e *CheckinInputError) Error() string { return e.Message }

func InvalidCheckin(message string) error { return &CheckinInputError{Message: message} }
