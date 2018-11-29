# git-gateway - Gateway to hosted git APIs

**Secure role based access to the APIs of common Git Hosting providers.**

When building sites with a JAMstack approach, a common pattern is to store all content as structured data in a Git repository instead of relying on an external database.

Netlify CMS is an open-source content management UI that allows content editors to work with your content in Git through a familiar content editing interface. This allows people to write and edit content without having to write code or know anything about Git, markdown, YAML, JSON, etc.

However, for most use cases you won’t want to require all content editors to have an account with full access to the source code repository for your website.

Netlify’s Git Gateway lets you setup a gateway to your choice of Git provider's API ( now available with both GitHub and GitLab 🎉 ) that lets tools like Netlify CMS work with content, branches and pull requests on your users’ behalf.

The Git Gateway works with some supported identity service that can issue JWTs and only allows access when a JSON Web Token with sufficient permissions is present.

To configure the gateway, see our `example.env` file

The Gateway limits access to the following sub endpoints of the repository:

for GitHub:
```
   /repos/:owner/:name/git/
   /repos/:owner/:name/contents/
   /repos/:owner/:name/pulls/
   /repos/:owner/:name/branches/
```
for GitLab:
```
   /repos/:owner/:name/files/
   /repos/:owner/:name/commits/
   /repos/:owner/:name/tree/
```

### Trying out `git-gateway`

The instructions below is a way of testing out `git-gateway`. It assumes you have Docker installed and are familiar with Okta (an IDaaS). If you are using a different stack, please adjust the steps accordingly.

1. pull down this project
2. generate a `personal access token` on github. (recommended: using a test account and w/ `repo:status` and `public_repo` permission only)
    https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/
3. `cp example.env my.env`
4. update `GITGATEWAY_GITHUB_ACCESS_TOKEN` value in `my.env` accordingly
5. update `GITGATEWAY_GITHUB_REPO` value in `my.env` (it will be where the content being stored, eg, `owner/netlify-cms-storage`.)
6. sign up for a Dev account on Okta: https://developer.okta.com/signup/
7. create a SPA Application onto the Dev account:
    a. fill out the details
    b. pick `Either Okta or App`
    c. pick `Send ID Token directly to app (Okta Simplified)``
    d. have redirect uri points to the url of your `my-netlify-cms` ip:port
      (eg, `http://localhost:8080/admin` etc, see, https://github.com/<< your org >>/my-netlify-cms)
    e. make sure `Authorization Servers` is activated
    f. go to `Trusted Origins` tab and add the url for your `my-netlify-cms` instance
    g. add yourself or a test user
8. update `ISSUER` value in `my.env` accordingly (eg, `https://dev-1234.oktapreview.com/oauth2/default`)
9. update `CLIENT_ID` value in `my.env` accordingly (eg, `32q897q234q324rq42322q`)
10. comment out `GITGATEWAY_ROLES` to disable role checking (authorization is controlled by `Assignments` on Okta)
11. update `GITGATEWAY_API_HOST` to `0.0.0.0`
12. inspect Dockerfile and then build the docker with this command:
    `docker build -t netlify/git-gateway:latest .`
13. run `git-gateway` with this command:
    `docker run --rm --env-file my.env -p 127.0.0.1:9999:9999 --expose 9999 -ti --name netlify-git-gateway "netlify/git-gateway:latest"`
14. update `config.yml` in your my-netlify-cms repo.
     change `backend.name` value to `git-gateway`
     change `backend.gateway_url` value to `http://localhost:9999`
15. integrate okta sign-in to your `my-netlify-cms` (eg, https://developer.okta.com/quickstart/#/widget/nodejs/express)
16. start your `my-netlify-cms` instance

See, Wiki page for additional information.
