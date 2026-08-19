package dto

// DeliveryCounts buckets a workspace's deliveries by lifecycle state.
// Sending counts sending deliveries on their first attempt; Retrying counts
// sending deliveries with at least one retry (Sending = sending∧attempt=0,
// Retrying = sending∧attempt>0).
type DeliveryCounts struct {
	Scheduled  int
	Sending    int
	Retrying   int
	Succeeded  int
	Failed     int
	Suppressed int
}

// DeliveryFilter narrows a delivery listing.
type DeliveryFilter struct {
	Status string
	Limit  int
	Offset int
}
