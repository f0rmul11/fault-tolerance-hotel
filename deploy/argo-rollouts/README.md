# Argo Rollouts v1.10.1

Локальная копия [официального install.yaml](https://github.com/argoproj/argo-rollouts/releases/download/v1.10.1/install.yaml), разделённая на `crds/` и `controller/`. Добавлен namespace `argo-rollouts`; namespace явно указан у ресурсов контроллера. Версия образа закреплена в Deployment.

hotelctl применяет CRD, ждёт их регистрации, применяет контроллер и ждёт его готовности. Полный набор CRD официального контроллера сохранён: создание AnalysisRun, Experiment или автоматического анализа в сценарий стенда не добавлено. Argo CD не требуется.
