package helpers

import (
	internalTypes "wuzapi/internal/types"

	"github.com/rs/zerolog/log"
)

func Find(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// Update entry in User map
func UpdateUserInfo(values interface{}, field string, value string) interface{} {
	log.Debug().Str("field", field).Str("value", value).Msg("User info updated")
	values.(internalTypes.Values).M[field] = value
	return values
}
