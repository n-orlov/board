window.addEventListener('load', function () {
  window.ui = SwaggerUIBundle({
    url: '/api/openapi.json',
    dom_id: '#swagger-ui',
    deepLinking: false,
    presets: [SwaggerUIBundle.presets.apis],
    layout: 'BaseLayout',
  });
});
