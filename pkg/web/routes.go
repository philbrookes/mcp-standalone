package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/feedhenry/mobile-server/pkg/mobile"
	"github.com/feedhenry/mobile-server/pkg/web/middleware"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	kerror "k8s.io/apimachinery/pkg/api/errors"
)

var (
	kubernetesOauthEndpoint = &oauth2.Endpoint{
		AuthURL:  "https://127.0.0.1:8443/oauth/authorize",
		TokenURL: "https://127.0.0.1:8443/oauth/token",
	}

	kubernetesOauthConfig = &oauth2.Config{
		RedirectURL:  "https://localhost:9000/oauth",
		ClientID:     "system:serviceaccount:myproject:mcp",
		ClientSecret: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJteXByb2plY3QiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlY3JldC5uYW1lIjoibWNwLXRva2VuLTFkcDdrIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQubmFtZSI6Im1jcCIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjExYmU2NzFiLTg2MDgtMTFlNy04ZDc1LTU0ZWU3NTg0ODk4OSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpteXByb2plY3Q6bWNwIn0.AE3A4fZat_cHigtnU2fNRr-8WGOUUhL5ET9Pvg-LplMI3PQwHfidjVVNjctbHZ8XPe_kvyv32sAeB_iJ1FvARSDfZpsphhRhOk9xYcs0bMscXh5BpkBJNskASjPqqDo07s1v-tL8s23PPr08SaPM110RXSj60yEn72IqXncY6pSXaqg2alkdy-wJqudmzL8ePQZXCrS14L97nqGFk7xnLRHwmRjB748u-wOjoT2M8oj_p9hUd0M0Mgg_-iIVq_XujtaVgncSfmMBv2jRtbMejuEbHLAa8zdBFdbHLFf64rUaZCfPfTMTeoVuJwXlIVqSK0hLDPXybO5MMAtmYyfEKQ",
		Scopes:       []string{"user:info"},
		Endpoint:     *kubernetesOauthEndpoint,
	}
)

// NewRouter sets up the HTTP Router
func NewRouter() *mux.Router {
	r := mux.NewRouter()
	r.StrictSlash(true)
	oauth2.RegisterBrokenAuthHeaderProvider(kubernetesOauthEndpoint.TokenURL)

	return r
}

// BuildHTTPHandler puts together our HTTPHandler
func BuildHTTPHandler(r *mux.Router, access *middleware.Access) http.Handler {
	recovery := negroni.NewRecovery()
	recovery.PrintStack = false
	n := negroni.New(recovery)
	cors := middleware.Cors{}
	n.UseFunc(cors.Handle)
	if access != nil {

		n.UseFunc(access.Handle)
	} else {
		fmt.Println("access control is turned off ")
	}
	n.UseHandler(r)
	return n
}

func handleOauthToken(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	sslcli := &http.Client{Transport: tr}
	ctx := context.TODO()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, sslcli)

	token, err := kubernetesOauthConfig.Exchange(ctx, code)
	if err != nil {
		fmt.Println("Code exchange failed with ", err)
		http.Redirect(w, r, fmt.Sprintf("%s/error?error=code_exchange_failed&error_description=%s", kubernetesOauthConfig.RedirectURL, err), http.StatusTemporaryRedirect)
		return
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(tokenJSON)

}

// OauthRoute configures & sets up the /oauth route.
func OauthRoute(r *mux.Router, handler *OauthHandler) {
	r.HandleFunc("/oauth/token", handleOauthToken)
}

// MobileAppRoute configure and setup the /mobileapp route. The middleware.Builder is responsible for building per request instances of clients
func MobileAppRoute(r *mux.Router, handler *MobileAppHandler) {
	r.HandleFunc("/mobileapp", handler.Create).Methods("POST")
	r.HandleFunc("/mobileapp/{id}", handler.Delete).Methods("DELETE")
	r.HandleFunc("/mobileapp/{id}", handler.Read).Methods("GET")
	r.HandleFunc("/mobileapp", handler.List).Methods("GET")
	r.HandleFunc("/mobileapp/{id}", handler.Update).Methods("PUT")
}

//SDKConfigRoute configures and sets up the /sdk routes
func SDKConfigRoute(r *mux.Router, handler *SDKConfigHandler) {
	r.HandleFunc("/sdk/mobileapp/{id}/config", handler.Read).Methods("GET")
}

// SysRoute congifures and sets up the /sys/* route
func SysRoute(r *mux.Router, handler *SysHandler) {
	r.HandleFunc("/sys/info/ping", handler.Ping).Methods("GET")
	r.HandleFunc("/sys/info/health", handler.Health).Methods("GET")
}

//TODO maybe better place to put this
func handleCommonErrorCases(err error, rw http.ResponseWriter, logger *logrus.Logger) {
	err = errors.Cause(err)
	if mobile.IsNotFoundError(err) {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	if e, ok := err.(*mobile.StatusError); ok {
		logger.Error(fmt.Sprintf("status error occurred %+v", err))
		http.Error(rw, err.Error(), e.StatusCode())
		return
	}
	if e, ok := err.(*kerror.StatusError); ok {
		logger.Error(fmt.Sprintf("kubernetes status error occurred %+v", err))
		http.Error(rw, e.Error(), int(e.Status().Code))
		return
	}
	logger.Error(fmt.Sprintf("unexpected and unknown error occurred %+v", err))
	http.Error(rw, err.Error(), http.StatusInternalServerError)
}
