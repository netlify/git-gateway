# git-gateway - Gateway to hosted git APIs

**Secure role based access to the APIs of common Git Hosting providers.**

When building sites with a JAMstack approach, a common pattern is to store all content as structured data in a Git repository instead of relying on an external database.

Netlify CMS is an open-source content management UI that allows content editors to work with your content in Git through a familiar content editing interface. This allows people to write and edit content without having to write code or know anything about Git, markdown, YAML, JSON, etc.

However, for most use cases you won’t want to require all content editors to have a GitHub account with full access to the source code repository for your website.

Netlify’s Git Gateway lets you setup a gateway to GitHub’s API (more providers coming) that lets tools like Netlify CMS work with content, branches and pull requests on your users’ behalf.

The Git Gateway works with any identity service that can issue JWTs and only allows access when a JSON Web Token with sufficient permissions is present.

To configure the gateway, see our example.env file

The Gateway limits access to the following sub endpoints of the repository:

    /repos/:owner/:name/git/
    /repos/:owner/:name/contents/
    /repos/:owner/:name/pulls/
    /repos/:owner/:name/branches/
