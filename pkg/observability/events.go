package observability

const (
	EventReasonMounted = "GitContentMounted"
	EventReasonDenied  = "GitContentDenied"
	EventReasonError   = "GitContentError"
)

type Event struct {
	Type    string
	Reason  string
	Message string
}
