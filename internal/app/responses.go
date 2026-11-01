package app

type PaginatedResponse[T any] struct {
	Items      []T   `json:"items"`
	TotalItems int64 `json:"totalItems"`
	TotalPages int64 `json:"totalPages"`
	PageSize   int64 `json:"pageSize"`
	Page       int64 `json:"page"`
}
