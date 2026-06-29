package admin

type Service struct{}

func NewService() Service {
	return Service{}
}

func (s Service) IsAdmin() bool {
	return isAdmin()
}

func (s Service) ElevateAndRestart() error {
	return elevateAndRestart()
}
