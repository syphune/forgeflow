FROM node:22-alpine AS build

WORKDIR /app
RUN npm install --global pnpm@10.14.0
ARG FORGEFLOW_BACKEND_URL=http://api:8080
ENV FORGEFLOW_BACKEND_URL=$FORGEFLOW_BACKEND_URL
ARG FORGEFLOW_UI_MODE=new
ENV FORGEFLOW_UI_MODE=$FORGEFLOW_UI_MODE
ARG NEXT_PUBLIC_FORGEFLOW_API_URL=
ENV NEXT_PUBLIC_FORGEFLOW_API_URL=$NEXT_PUBLIC_FORGEFLOW_API_URL
ARG NEXT_PUBLIC_FORGEFLOW_DEV_AUTH=false
ENV NEXT_PUBLIC_FORGEFLOW_DEV_AUTH=$NEXT_PUBLIC_FORGEFLOW_DEV_AUTH
ARG NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID=
ENV NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID=$NEXT_PUBLIC_FORGEFLOW_ORGANIZATION_ID
ARG NEXT_PUBLIC_FORGEFLOW_ACTOR_ID=
ENV NEXT_PUBLIC_FORGEFLOW_ACTOR_ID=$NEXT_PUBLIC_FORGEFLOW_ACTOR_ID
ARG NEXT_PUBLIC_FORGEFLOW_PROJECT_ID=
ENV NEXT_PUBLIC_FORGEFLOW_PROJECT_ID=$NEXT_PUBLIC_FORGEFLOW_PROJECT_ID

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY apps/web/package.json apps/web/package.json
COPY apps/desktop/package.json apps/desktop/package.json
COPY packages/api-client/package.json packages/api-client/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN pnpm install --frozen-lockfile

COPY apps/web apps/web
COPY packages packages
RUN mkdir -p apps/web/public
RUN pnpm --filter @forgeflow/web build

FROM node:22-alpine AS runner

WORKDIR /app
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000

COPY --from=build /app/apps/web/.next/standalone ./
COPY --from=build /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=build /app/apps/web/public ./apps/web/public

CMD ["node", "apps/web/server.js"]
