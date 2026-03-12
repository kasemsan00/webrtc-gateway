# syntax=docker/dockerfile:1

FROM node:22-alpine AS base
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable

FROM base AS deps
WORKDIR /app
# Copy lockfile from root and frontend package.json
COPY pnpm-lock.yaml ./
COPY apps/frontend/package.json ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --no-frozen-lockfile


FROM base AS build
WORKDIR /app
ARG VITE_GATEWAY_URL
ARG VITE_TURN_URL
ARG VITE_TURN_USERNAME
ARG VITE_TURN_CREDENTIAL
ARG VITE_KEYCLOAK_URL
ARG VITE_KEYCLOAK_REALM
ARG VITE_KEYCLOAK_CLIENT
ARG VITE_CONFIG_AUTORECORD
ENV VITE_GATEWAY_URL=${VITE_GATEWAY_URL}
ENV VITE_TURN_URL=${VITE_TURN_URL}
ENV VITE_TURN_USERNAME=${VITE_TURN_USERNAME}
ENV VITE_TURN_CREDENTIAL=${VITE_TURN_CREDENTIAL}
ENV VITE_KEYCLOAK_URL=${VITE_KEYCLOAK_URL}
ENV VITE_KEYCLOAK_REALM=${VITE_KEYCLOAK_REALM}
ENV VITE_KEYCLOAK_CLIENT=${VITE_KEYCLOAK_CLIENT}
ENV VITE_CONFIG_AUTORECORD=${VITE_CONFIG_AUTORECORD}
COPY --from=deps /app/node_modules ./node_modules
COPY apps/frontend .
RUN pnpm run build

FROM node:22-alpine AS runner
ENV NODE_ENV=production
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/dist/server ./dist/server
COPY --from=build /app/dist/client ./dist/client
COPY apps/frontend/docker-server.mjs ./docker-server.mjs
COPY apps/frontend/package.json ./ 
EXPOSE 4173
CMD ["node", "docker-server.mjs"]


