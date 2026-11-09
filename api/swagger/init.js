SwaggerUIBundle({
  dom_id: "#swagger-ui",
  url: "./openapi.json",
  validatorUrl: null,
  queryConfigEnabled: false,
  persistAuthorization: false,
  supportedSubmitMethods: ["get", "head"],
  requestInterceptor(request) {
    const method = (request.method || "GET").toUpperCase();
    if (method !== "GET" && method !== "HEAD") {
      throw new Error("PandaOPDS API documentation only sends read requests");
    }
    return request;
  },
});
