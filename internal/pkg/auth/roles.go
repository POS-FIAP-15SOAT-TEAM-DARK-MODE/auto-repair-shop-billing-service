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
	MechanicRoles             = []Role{MECHANIC, ADMIN}
	CustomerRoles             = []Role{CUSTOMER, ADMIN}
	AttendantAndMechanicRoles = []Role{ATTENDANT, MECHANIC, ADMIN}
)
