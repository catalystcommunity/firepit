# Firepit web application

This module contains the SolidJS single-page application. It uses the
generated TypeScript CSIL client in `src/gen`.

Run commands from the repository root when possible:

```sh
./tools.sh test-web
./tools.sh lint-web
./tools.sh dev
```

For direct web development, run:

```sh
cd webapp
npm install
npm run dev
```

Set `VITE_FIREPIT_MOCK=1` to use the in-memory transport. Without this value,
the application sends CSIL-RPC requests to the API.

Do not edit files in `src/gen`. Change the CSIL schema and run
`./tools.sh gen`.
