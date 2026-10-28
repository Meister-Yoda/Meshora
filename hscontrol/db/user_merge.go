package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MergeUsers is an explicit administrative transfer; the target UID survives.
var (
	errMerge1 = errors.New("quelle und ziel müssen verschieden sein")
	errMerge2 = errors.New("unterschiedliche E-Mail-Adressen zuerst prüfen und korrigieren")
	errMerge3 = errors.New("zwei verschiedene OIDC-Identitäten können nicht verbunden werden")
	errMerge4 = errors.New("zwei lokale Legacy-Anmeldungen können nicht verbunden werden")
	errMerge5 = errors.New("beide Konten haben ein Passwort; das Beibehalten des Zielpassworts muss ausdrücklich bestätigt werden")
)

func (hsdb *HSDatabase) MergeUsers(sourceID, targetID types.UserID, keepTargetPassword bool) (*types.User, error) {
	if sourceID == targetID {
		return nil, errMerge1
	}

	return Write(hsdb.DB, func(tx *gorm.DB) (*types.User, error) {
		source, err := GetUserByID(tx, sourceID)
		if err != nil {
			return nil, err
		}

		target, err := GetUserByID(tx, targetID)
		if err != nil {
			return nil, err
		}

		var secured int64

		err = tx.Model(&types.UserPasskey{}).Where("user_id = ?", sourceID).Count(&secured).Error
		if err != nil {
			return nil, err
		}

		if secured > 0 {
			return nil, ErrSecurityMerge
		}

		var security types.UserSecurity

		err = tx.First(&security, "user_id = ?", sourceID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		if len(security.EncryptedTOTPSecret) > 0 {
			return nil, ErrSecurityMerge
		}

		sourcePolicy, policyErr := userAuthPolicy(tx, sourceID)
		if policyErr != nil {
			return nil, policyErr
		}

		if sourcePolicy.EmailChangeBlocked || sourcePolicy.RequireStrongAuth || sourcePolicy.TOTPBypassedAt(time.Now()) || sourcePolicy.PasskeysSuspendedAt(time.Now()) {
			return nil, ErrSecurityMerge
		}
		var accounts []types.UserAccount

		query := tx.Where("user_id IN ?", []types.UserID{sourceID, targetID}).Order("user_id")
		if tx.Name() == types.DatabasePostgres {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}

		err = query.Find(&accounts).Error
		if err != nil {
			return nil, err
		}

		var from, to types.UserAccount

		from.UserID, to.UserID = uint64(sourceID), uint64(targetID)
		for _, account := range accounts {
			if account.UserID == uint64(sourceID) {
				from = account
			} else {
				to = account
			}
		}

		err = validateAccountMerge(source, target, from, to, keepTargetPassword)
		if err != nil {
			return nil, err
		}

		if target.Email == "" {
			target.Email = source.Email
		}

		email, err := NormalizeAccountEmail(target.Email)
		if err != nil {
			return nil, err
		}

		target.Email = email
		if !target.ProviderIdentifier.Valid && source.ProviderIdentifier.Valid {
			target.ProviderIdentifier = source.ProviderIdentifier
			target.Provider = source.Provider
			target.ProfilePicURL = source.ProfilePicURL
		}

		if target.DisplayName == "" {
			target.DisplayName = source.DisplayName
		}

		if len(to.PasswordHash) == 0 {
			to.PasswordHash = from.PasswordHash
			to.MustChangePassword = from.MustChangePassword
		}

		if !to.LegacyLogin.Valid {
			to.LegacyLogin = from.LegacyLogin
		}

		to.IsAdmin = to.IsAdmin || from.IsAdmin
		to.Revision++
		to.LoginEmail = sql.NullString{String: email, Valid: true}
		// Release unique identity/login keys before transferring them. Everything
		// rolls back together on conflict; foreign keys remain enabled throughout.
		err = tx.Model(source).Update("provider_identifier", nil).Error
		if err != nil {
			return nil, err
		}

		err = tx.Where("user_id = ?", sourceID).Delete(&types.UserAccount{}).Error
		if err != nil {
			return nil, err
		}

		err = tx.Model(&types.Node{}).Where("user_id = ?", sourceID).Update("user_id", targetID).Error
		if err != nil {
			return nil, err
		}

		err = tx.Model(&types.Credential{}).Where("user_id = ?", sourceID).Update("user_id", targetID).Error
		if err != nil {
			return nil, err
		}

		err = tx.Unscoped().Delete(source).Error
		if err != nil {
			return nil, err
		}

		err = tx.Save(target).Error
		if err != nil {
			return nil, err
		}

		err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, UpdateAll: true}).Create(&to).Error
		if err != nil {
			return nil, err
		}

		return target, nil
	})
}

func validateAccountMerge(source, target *types.User, from, to types.UserAccount, keepTargetPassword bool) error {
	if source.Email != "" && target.Email != "" && !strings.EqualFold(source.Email, target.Email) {
		return errMerge2
	}

	if source.ProviderIdentifier.Valid && target.ProviderIdentifier.Valid && source.ProviderIdentifier.String != target.ProviderIdentifier.String {
		return errMerge3
	}

	if from.LegacyLogin.Valid && to.LegacyLogin.Valid && from.LegacyLogin.String != to.LegacyLogin.String {
		return errMerge4
	}

	if len(from.PasswordHash) > 0 && len(to.PasswordHash) > 0 && !keepTargetPassword {
		return errMerge5
	}

	return nil
}
