# go-project-client-server

## Пояснение краеугольных аспектов
- “каждый может отправлять по 5 запросов на сервер” - я понимаю как то, что пачка из 5 запросов определенного воркера должна отправляться (т.е. в нее не могут затесаться запросы другого воркера), это в целом было подтверждено в беседе.
- “Если лимит превышен, то возвращать уникальное сообщение о том, что лимит превышен” - я понимаю как то, что если клиент переборщил с запросами, то сервер ему об этом скажет. Мы работаем с HTTP-сервером, статус ответа входит в HTTP-ответ, так что в таких случаях превышения лимита в моей программе сервер будет отвечать клиенту 429 Too Many Requests. Соответственно при выводе статистики по статусам у клиентов в списке статусов будет и этот статус, что репрезентативно для просмотра результатов.
- В условии в 6-м пункте говорится о лимите, но не говорится что делать с запросами, которые как раз не прошли по лимиту. В моей программе сервер ответит 429 Too Many Requests, и такие запросы повторно отправляться не будут. 
- JSON для GET-запроса мы получаем “потом” - после работы клиентов ⇒ чтобы, сервер не крутился бесконечно, и мы убедились, что программа успешно завершает работу, мы выделим на это 30 секунд.
- Согласно условию за секунду клиенты шлют по 5 запросов, а сервер обрабатывает 5 запросов, т.е. в JSON логично ожидать суммарное кол-во положительных и отрицательных статусов, кратным 5. В идеале конечно должно быть 100 *(сервер же принимает 5 запросов в секунду, в секунду от каждого клиента ему приходит по 5 запросов. Клиенты всего шлют по 100 запросов, в секунду обрабатывается с положительным (200, 202) / отрицательным (400, 500) статусом 5 запросов (у остальных 5 – статус 429 Too Many Requests - см. пункт 1 и 3), т.е. через 20 секунд в идеале должны прекратиться запросы клиентов)*. Но понятно, что из-за особенностей работы планировщика Go мб задержки при формировании запросов и использовании мьютексов (у меня в программе они и используются как раз), так что ждем суммарное количество положительных и отрицательных статусов в JSON кратным 5 и >= 100.
- **P.S. Для 1000 запросов протестировал - всё отлично синхронизируется и 1000 запросов клиент каждый отправляет по 5 в секунду.**
