const http = require("http");
http.createServer((_, res) => res.end("ok\n")).listen(3000);
