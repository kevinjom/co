# co docs

Handbook for [`co`](https://github.com/kevinjom/co). Pages live in `src/content/docs/`.

```sh
npm install
npm run dev
```

`npm run build` writes `dist/` for `https://kevinjom.github.io/co/`. A stable tag `vX.Y.Z` publishes the three newest stable tags that contain this directory. The newest is served at the site root and at `/vX.Y.Z/`. Each older tag is only under its own prefix. Pull requests build the site and do not deploy.
