package internalTypes

type Values struct {
	M map[string]string
}

func (v Values) Get(key string) string {
	return v.M[key]
}