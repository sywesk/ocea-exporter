package oceaapi

type Resident struct {
	CodeClient    string `json:"codeClient"`
	NomClient     string `json:"nomClient"`
	Notifications []struct {
		ConfigurationNotification struct {
			NotifMail bool `json:"notifMail"`
			NotifSMS  bool `json:"notifSMS"`
		} `json:"configurationNotification"`
		TypeNotificationResident string `json:"typeNotificationResident"`
	} `json:"notifications"`
	Occupations []struct {
		CodeSite       string `json:"codeSite"`
		DateDebut      string `json:"dateDebut"`
		DateFin        string `json:"dateFin"`
		LogementID     string `json:"logementId"`
		ResidentID     string `json:"residentId"`
		TypeOccupation string `json:"typeOccupation"`
	} `json:"occupations"`
	Resident struct {
		Civilite                 string `json:"civilite"`
		DateAccountDisabled      string `json:"dateAccountDisabled"`
		DateLastConnection       string `json:"dateLastConnection"`
		Email                    string `json:"email"`
		ID                       string `json:"id"`
		IsWelcomeModaleDisplayed bool   `json:"isWelcomeModaleDisplayed"`
		Nom                      string `json:"nom"`
		Prenom                   string `json:"prenom"`
		Telephone                string `json:"telephone"`
	} `json:"resident"`
}

type Dashboard struct {
	Fluide                    string  `json:"fluide"`
	LocalID                   string  `json:"localId"`
	ConsoMoyenne              float64 `json:"consoMoyenne"`
	ConsoDernierMois          float64 `json:"consoDernierMois"`
	ConsoCumuleeAnneeCourante float64 `json:"consoCumuleeAnneeCourante"`
	Unite                     string  `json:"unite"`
	ConsoMoisCourant          float64 `json:"consoMoisCourant"`
	DateDerniereReleve        string  `json:"dateDerniereReleve"`
}

type Fluid struct {
	Fluide           string `json:"fluide"`
	TypeDistribution string `json:"typeDistribution"`
	TypeReleve       string `json:"typeReleve"`
}

type Local struct {
	FluidesRestitues []Fluid `json:"fluidesRestitues"`
	Local            struct {
		Adresse struct {
			CodePostal string `json:"codePostal"`
			Complement string `json:"complement"`
			NumeroRue  string `json:"numeroRue"`
			Pays       string `json:"pays"`
			Ville      string `json:"ville"`
		} `json:"adresse"`
		Batiment             string  `json:"batiment"`
		CodeSite             string  `json:"codeSite"`
		Etage                string  `json:"etage"`
		ID                   string  `json:"id"`
		IdentificationLocal  string  `json:"identificationLocal"`
		IsPatrimoineHarmonie bool    `json:"isPatrimoineHarmonie"`
		NumeroLot            string  `json:"numeroLot"`
		NumeroPorte          string  `json:"numeroPorte"`
		ReferenceClient      string  `json:"referenceClient"`
		Tantieme             float64 `json:"tantieme"`
		Type                 string  `json:"type"`
		Usage                string  `json:"usage"`
	} `json:"local"`
}

type Device struct {
	AppareilID             string  `json:"appareilId"`
	Date                   string  `json:"date"`
	Emplacement            string  `json:"emplacement"`
	Fluide                 string  `json:"fluide"`
	NumeroCompteurAppareil string  `json:"numeroCompteurAppareil"`
	Unite                  string  `json:"unite"`
	ValeurIndex            float64 `json:"valeurIndex"`
}
