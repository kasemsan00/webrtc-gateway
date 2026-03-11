# syntax=docker/dockerfile:1.5

FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json ./
RUN npm i

FROM node:22-alpine AS build
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
COPY . .
RUN npm run build

FROM node:22-alpine AS runner
ENV NODE_ENV=production
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/dist/server ./dist/server
COPY --from=build /app/dist/client ./dist/client
COPY docker-server.mjs ./docker-server.mjs
COPY package.json ./ 
EXPOSE 4173
CMD ["node", "docker-server.mjs"]
