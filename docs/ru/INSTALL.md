# Установка

Problem Desk - приложение-плагин для Grafana 13.3 и новее. Установите пакет плагина на сервер Grafana, затем включите приложение в Grafana. ID плагина: `mlimarenko-problemdesk-app`. Первый бинарный пакет предназначен для Linux amd64.

Плагин не подписан. Только для разработки на собственном сервере Grafana можно настроить на загрузку именно этого неподписанного плагина: `allow_loading_unsigned_plugins = mlimarenko-problemdesk-app` в разделе `[plugins]`. После изменения конфигурации перезапустите Grafana. Не разрешайте загрузку всех неподписанных плагинов.

Загрузите пакет Linux amd64 и проверьте контрольную сумму до распаковки:

```sh
VERSION=0.2.2
ASSET="mlimarenko-problemdesk-app-${VERSION}.zip"
BASE="https://github.com/mlimarenko/grafana-problem-desk/releases/download/v${VERSION}"
curl -fLO "${BASE}/${ASSET}"
curl -fLO "${BASE}/${ASSET}.sha256"
sha256sum -c "${ASSET}.sha256"
sudo unzip "${ASSET}" -d /var/lib/grafana/plugins
```

Если в вашей Grafana настроен другой каталог плагинов, используйте его. Сервисный пользователь Grafana должен иметь права чтения файлов и запуска `gpx_problemdesk_linux_amd64`. При использовании Docker подключите каталог плагина как volume. Постоянное хранилище backend расположено отдельно от файлов плагина; его нельзя оставлять во временном слое контейнера.

Включите приложение в разделе управления плагинами Grafana и откройте Problem Desk. Для развертывания необходимо настроить доступ к данным оповещений и постоянному хранилищу; см. [настройку](CONFIGURATION.md) и [безопасность](SECURITY.md).
