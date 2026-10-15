# 构建阶段：vite 产物
FROM node:22-alpine AS build
WORKDIR /src

COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY . .
RUN npm run build

# 运行阶段：Nginx 托管静态资源并反代 /api
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
