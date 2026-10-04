package main

import "github.com/vekhyat/Auralis/backend"

type SourceVerificationView struct {
	backend.VerificationPresentation
	CanConfirm bool `json:"can_confirm"`
}

func sourceVerificationView(p backend.VerificationPresentation) SourceVerificationView {
	return SourceVerificationView{VerificationPresentation: p, CanConfirm: backend.SourceVerificationNeedsConfirmation()}
}

func (a *App) GetSourceVerification() SourceVerificationView {
	return sourceVerificationView(backend.GetVerificationPresentation())
}

func (a *App) SetSourceVerificationViewport(id uint64, left, top, width, height int) error {
	return backend.SetVerificationViewport(id, left, top, width, height)
}

func (a *App) CancelSourceVerification(id uint64) error {
	return backend.CancelInAppVerification(id)
}

func (a *App) ConfirmSourceVerification() (bool, error) {
	return backend.ConfirmSourceVerification()
}

func (a *App) GetSourceConnections() []backend.SourceConnection { return backend.SourceConnections() }
func (a *App) CheckSourceConnection(id string) (backend.SourceConnection, error) {
	return backend.CheckSourceConnection(id)
}
func (a *App) VerifySourceConnection(id string) (backend.SourceConnection, error) {
	return backend.VerifySourceConnection(id)
}
func (a *App) VerifyCommunitySource(id string) (backend.CommunitySourceCheck, error) {
	return backend.VerifyCommunitySource(id)
}
