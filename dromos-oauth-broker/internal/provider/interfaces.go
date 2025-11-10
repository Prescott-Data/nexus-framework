package provider

import (
    "github.com/google/uuid"
)

// ProfileList is the (id, name) struct for the List method
type ProfileList struct {
    ID   string `db:"id" json:"id"`
    Name string `db:"name" json:"name"`
}

// ProfileStorer defines the store's behavior for the provider handler.
type ProfileStorer interface {
    RegisterProfile(profileJSON string) (*Profile, error)
    GetProfile(id uuid.UUID) (*Profile, error)
    GetProfileByName(name string) (*Profile, error)
    UpdateProfile(p *Profile) error
    DeleteProfile(id uuid.UUID) error
    ListProfiles() ([]ProfileList, error)
}
