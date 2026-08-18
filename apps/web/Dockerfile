# syntax=docker/dockerfile:1.7

FROM node:24.18.0-alpine AS toolchain

ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable && corepack prepare pnpm@11.19.0 --activate

FROM toolchain AS dependencies

WORKDIR /src
# NPM_REGISTRY is overridable at build time for restricted networks (see
# docs/iterations/ITER-0002/decisions.md, D10); the default is upstream.
ARG NPM_REGISTRY=https://registry.npmjs.org/
RUN pnpm config set registry "${NPM_REGISTRY}"
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json ./apps/web/package.json
RUN pnpm install --frozen-lockfile

FROM dependencies AS build

COPY apps/web ./apps/web
ENV NEXT_TELEMETRY_DISABLED=1
# Next.js bakes rewrite destinations into the routes manifest at build
# time, so the internal API URL travels as a build argument.
ARG API_INTERNAL_URL=http://localhost:8080
ENV API_INTERNAL_URL=${API_INTERNAL_URL}
RUN pnpm --filter @artificial-brain/web build

FROM node:24.18.0-alpine AS runtime

WORKDIR /app
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=build --chown=node:node /src/apps/web/.next/standalone ./
COPY --from=build --chown=node:node /src/apps/web/.next/static ./apps/web/.next/static

USER node
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
