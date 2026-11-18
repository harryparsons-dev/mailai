package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"mailai/database/models"
	"mailai/pkg/responses"
	"mailai/server"
	"mailai/server/requests"
	userresponses "mailai/server/responses"
	tokenservice "mailai/services/tokenService"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	server       *server.Server
	tokenService *tokenservice.TokenService
}

func NewAuthHandler(server *server.Server) *AuthHandler {
	return &AuthHandler{
		server:       server,
		tokenService: tokenservice.NewTokenService(server.JWT),
	}
}

func (h *AuthHandler) SignUp(c *echo.Context) error {
	ctx := c.Request().Context()
	request := &requests.SignUpRequest{}
	if err := c.Bind(request); err != nil {
		return responses.Error(c, http.StatusBadRequest, "Invalid request payload")
	}
	if err := request.Validate(); err != nil {
		return responses.Error(c, http.StatusBadRequest, err.Error())
	}

	exists, err := h.server.Db.NewSelect().Model((*models.User)(nil)).Where("email = ?", request.Email).Exists(ctx)
	if err != nil {
		log.Printf("Error checking for existing user while signing up: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error signing up user")
	}
	if exists {
		return responses.Error(c, http.StatusBadRequest, "User with this email already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password while signing up: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error signing up user")
	}

	user := &models.User{
		Email:        request.Email,
		PasswordHash: string(hash),
	}
	if _, err := h.server.Db.NewInsert().Model(user).Exec(ctx); err != nil {
		log.Printf("Error creating user while signing up: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error signing up user")
	}

	return responses.Success(c, http.StatusCreated, "Signed up successfully")
}

func (h *AuthHandler) Login(c *echo.Context) error {
	ctx := c.Request().Context()
	request := &requests.LoginRequest{}
	if err := c.Bind(request); err != nil {
		return responses.Error(c, http.StatusBadRequest, "Invalid request payload")
	}
	if err := c.Validate(request); err != nil {
		return responses.Error(c, http.StatusBadRequest, "Email and password are required")
	}

	user := &models.User{}
	err := h.server.Db.NewSelect().Model(user).Where("email = ?", request.Email).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return responses.Error(c, http.StatusUnauthorized, "Invalid email or password")
	}
	if err != nil {
		log.Printf("Error looking up user while logging in: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error logging in user")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)); err != nil {
		return responses.Error(c, http.StatusUnauthorized, "Invalid email or password")
	}

	accessToken, err := h.server.JWT.CreateAccessToken(user.ID)
	if err != nil {
		log.Printf("Error signing access token while logging in: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error logging in user")
	}
	refreshToken, err := h.server.JWT.CreateRefreshToken(user.ID)
	if err != nil {
		log.Printf("Error signing refresh token while logging in: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error logging in user")
	}

	refreshTokenHash := sha256.Sum256([]byte(refreshToken))
	userToken := &models.UserToken{
		Token:  hex.EncodeToString(refreshTokenHash[:]),
		UserId: user.ID,
	}
	if _, err := h.server.Db.NewInsert().Model(userToken).Exec(ctx); err != nil {
		log.Printf("Error storing refresh token while logging in: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error logging in user")
	}

	h.tokenService.SetAccessCookie(c, accessToken)
	h.tokenService.SetRefreshCookie(c, refreshToken)

	return responses.Success(c, http.StatusOK, "Logged in successfully")
}

func (h *AuthHandler) Refresh(c *echo.Context) error {
	ctx := c.Request().Context()
	cookie, err := c.Request().Cookie("RefreshToken")
	if err != nil {
		return responses.Error(c, http.StatusUnauthorized, "Refresh token required")
	}

	userID, err := h.server.JWT.VerifyRefreshToken(cookie.Value)
	if err != nil {
		return responses.Error(c, http.StatusUnauthorized, "Invalid or expired refresh token")
	}

	user := &models.User{}
	err = h.server.Db.NewSelect().Model(user).Where("id = ?", userID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return responses.Error(c, http.StatusUnauthorized, "User not found")
	}
	if err != nil {
		log.Printf("Error looking up user while refreshing token: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error refreshing token")
	}

	accessToken, err := h.server.JWT.CreateAccessToken(user.ID)
	if err != nil {
		log.Printf("Error signing refreshed token: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error refreshing token")
	}
	refreshToken, err := h.server.JWT.CreateRefreshToken(user.ID)
	if err != nil {
		log.Printf("Error signing rotated refresh token: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error refreshing token")
	}

	oldRefreshTokenHash := sha256.Sum256([]byte(cookie.Value))
	newRefreshTokenHash := sha256.Sum256([]byte(refreshToken))
	tokenRotated := false
	err = h.server.Db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewDelete().Model((*models.UserToken)(nil)).
			Where("token = ?", hex.EncodeToString(oldRefreshTokenHash[:])).
			Where("user_id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted == 0 {
			return nil
		}

		userToken := &models.UserToken{
			Token:  hex.EncodeToString(newRefreshTokenHash[:]),
			UserId: user.ID,
		}
		if _, err := tx.NewInsert().Model(userToken).Exec(ctx); err != nil {
			return err
		}
		tokenRotated = true
		return nil
	})
	if err != nil {
		log.Printf("Error rotating refresh token: %v", err)
		return responses.Error(c, http.StatusInternalServerError, "Error refreshing token")
	}
	if !tokenRotated {
		return responses.Error(c, http.StatusUnauthorized, "Invalid or expired refresh token")
	}

	h.tokenService.SetAccessCookie(c, accessToken)
	h.tokenService.SetRefreshCookie(c, refreshToken)

	return responses.Data(c, http.StatusOK, userresponses.NewUserResponse(user))
}

func (h *AuthHandler) Logout(c *echo.Context) error {
	ctx := c.Request().Context()
	cookie, err := c.Request().Cookie("RefreshToken")
	if err == nil {
		refreshTokenHash := sha256.Sum256([]byte(cookie.Value))
		_, err := h.server.Db.NewDelete().Model((*models.UserToken)(nil)).
			Where("token = ?", hex.EncodeToString(refreshTokenHash[:])).
			Exec(ctx)
		if err != nil {
			log.Printf("Error deleting refresh token while logging out: %v", err)
			return responses.Error(c, http.StatusInternalServerError, "Error logging out user")
		}
	}

	h.tokenService.ClearAuthCookies(c)

	return responses.Success(c, http.StatusOK, "Signed out successfully")
}
