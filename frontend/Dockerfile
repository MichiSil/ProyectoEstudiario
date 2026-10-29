# Frontend de Estudiario: se compila con Node y se sirve con nginx, que además
# reenvía /api al api-gateway (el navegador ve un único origen, sin CORS).
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
