package auth

type Role string

const (
	ADMIN     Role = "ADMIN"
	ATTENDANT Role = "ATTENDANT"
	MECHANIC  Role = "MECHANIC"
	CUSTOMER  Role = "CUSTOMER"
)

var (
	AttendantRoles            = []Role{ATTENDANT, ADMIN}
	AttendantAndMechanicRoles = []Role{ATTENDANT, MECHANIC, ADMIN}
)
