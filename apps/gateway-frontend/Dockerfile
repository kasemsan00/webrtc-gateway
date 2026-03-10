# syntax=docker/dockerfile:1.5

FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json ./
RUN npm i

FROM node:22-alpine AS build
WORKDIR /app
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
