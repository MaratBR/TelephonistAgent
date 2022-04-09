package locales

type Messages struct {
	APIURLIs      string `json:"ApiUrlIs"`
	APIURLMissing string `json:"ApiUrlMissing"`
	Cli           struct {
		Actions struct {
			Info          string `json:"Info"`
			Init          string `json:"Init"`
			Main          string `json:"Main"`
			RefetchConfig string `json:"RefetchConfig"`
		} `json:"Actions"`
		Flag struct {
			ConfigFolderPath string `json:"ConfigFolderPath"`
			Restart          string `json:"Restart"`
			Secure           string `json:"Secure"`
			Verbose          string `json:"Verbose"`
		} `json:"Flag"`
	} `json:"CLI"`
	CreateService struct {
		CreatingGroup                      string `json:"CreatingGroup"`
		CreatingUser                       string `json:"CreatingUser"`
		FailedToChmodDir                   string `json:"FailedToChmodDir"`
		FailedToChownDir                   string `json:"FailedToChownDir"`
		FailedToCopyExecutable             string `json:"FailedToCopyExecutable"`
		FailedToCreateGroup                string `json:"FailedToCreateGroup"`
		FailedToCreateUser                 string `json:"FailedToCreateUser"`
		FailedToModifyDirectoryPermissions string `json:"FailedToModifyDirectoryPermissions"`
		FileIsAlreadyInPlace               string `json:"FileIsAlreadyInPlace"`
		ModifyingDirectoryPermissions      string `json:"ModifyingDirectoryPermissions"`
	} `json:"CreateService"`
	DisplayNameOrEmpty         string `json:"DisplayNameOrEmpty"`
	DomainNameNote             string `json:"DomainNameNote"`
	EnterAPIURL                string `json:"EnterApiUrl"`
	FailedToConnectToTheServer string `json:"FailedToConnectToTheServer"`
	FailedToWriteConfig        string `json:"FailedToWriteConfig"`
	FinalConfigIs              string `json:"FinalConfigIs"`
	InputKey                   string `json:"InputKey"`
	InputNewKeyOrEmpty         string `json:"InputNewKeyOrEmpty"`
	InputNewURLOrEmpty         string `json:"InputNewUrlOrEmpty"`
	InvalidURL                 string `json:"InvalidURL"`
	IsThatOkay                 string `json:"IsThatOkay"`
	KeyIs                      string `json:"KeyIs"`
	KeyMissing                 string `json:"KeyMissing"`
	N                          string `json:"N"`
	Name                       string `json:"Name"`
	PleaseGoToCR               string `json:"PleaseGoToCR"`
	Tags                       string `json:"Tags"`
	TryRunInit                 string `json:"TryRunInit"`
	WantCR                     string `json:"WantCR"`
	Y                          string `json:"Y"`
	Yn                         string `json:"YN"`
}
