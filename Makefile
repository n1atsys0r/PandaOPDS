.PHONY: webui

webui:
	cd frontend && npm ci && npm run build
