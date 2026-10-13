# Deliberately flawed sample. Do not copy.
FROM node:latest
WORKDIR /app
COPY . .
RUN npm install
ENV API_TOKEN=replace-me-in-prod
EXPOSE 3000
CMD ["node", "app/server.js"]
