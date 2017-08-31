package gateways

import "net/http"

// GitHubGateway acts as a proxy to GitHub
type GitHubGateway struct {
}

func (gh *GitHubGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {

}
