package handlers

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createStoreRequest struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Description   string                 `json:"description,omitempty"`
	Schema        map[string]interface{} `json:"schema,omitempty"`
	NamingPattern string                 `json:"namingPattern,omitempty"`
}
