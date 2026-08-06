package model

type ScenarioContextInput struct {
	Situation          string `json:"situation"`
	UserRole           string `json:"user_role"`
	CounterpartRole    string `json:"counterpart_role"`
	Goal               string `json:"goal"`
	CounterpartPersona string `json:"counterpart_persona"`
}

type ScenarioDefaults struct {
	Situation          string
	UserRole           string
	CounterpartRole    string
	Goal               string
	CounterpartPersona string
}

type ScenarioContextSnapshot struct {
	Situation          string `json:"situation"`
	UserRole           string `json:"user_role"`
	CounterpartRole    string `json:"counterpart_role"`
	Goal               string `json:"goal"`
	CounterpartPersona string `json:"counterpart_persona"`
}
