package models

type Request struct {
	URL string `json:"url"`
}
type Response struct {
	Result string `json:"result"`
}

type BatchRequest struct {
	Correlation_id string `json:"correlation_id"`
	Original_url   string `json:"original_url"`
}

type BatchResponse struct {
	Correlation_id string `json:"correlation_id"`
	Short_url      string `json:"short_url"`
}
