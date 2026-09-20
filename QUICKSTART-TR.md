# Hızlı Kurulum

## Projeyi anlatarak başlatma

Harness kurulu hedef repo içinde GitHub (`gh auth login`, `gh auth refresh -s
project`) ve Claude CLI oturumu açık olmalı. Bootstrap planlama şu anda araçları
tamamen kapatılmış Claude ile çalışır. Coordinator farklıysa runtime.env içinde
`HARNESS_PLANNER_PROVIDER="claude"` seçin. Codex/Gemini worker olarak kullanılabilir.
Repo GitHub'da
mevcut olmalı; worker'lar için ilk commit push edilmiş olmalı.

```bash
.ai-team/bin/bootstrap-project --brief PROJECT.md --start
```

`PROJECT.md` içine proje amacını, kapsamını ve kısıtlarını yazın; issue oluşturmanız
gerekmez. Dosya vermeden `.ai-team/bin/bootstrap-project` çalıştırırsanız editör
açılır. `--brief -` stdin'den okur. `--title "Vensift"` Project adını belirler.
Planlama agenti faz/feature/task/subtask planını üretir; publisher GitHub Project,
issue, native parent/dependency ilişkileri ve başlangıç agent/model alanlarını
oluşturur. Graph tamamlanınca uygun task'ler READY olur ve `--start` ilk deterministik
broker turunu çalıştırır. Sonraki READY dispatch turları için
`.ai-team/bin/coordinator-cycle` kullanın. PR/review/merge/DONE aşamaları bu güvenli
increment'te henüz otomatik değildir.

Mevcut `HARNESS_PROJECT_NUMBER` seçili Project'i kullanır; yeni Project oluşturmak
için bu değeri boş bırakın. Başarısızlıkta aynı brief ile tekrar çalıştırın; plan
ve issue'lar GitHub'dan kurtarılır, tekrar üretilmez. Brief'e şifre veya anahtar
koymayın. `.ai-team/bin/migrate-secrets` ile repo dışında oluşturulan secret store'daki
`HARNESS_BOOTSTRAP_HMAC_KEY` imza anahtarını güvenli yedekleyin. Anahtar kaybolursa
mevcut planların güvenli kurtarılması için aynı anahtar geri yüklenmelidir.
Bootstrap imza anahtarı oluşturulunca implementer'lar yalnız imzalı bootstrap
görevlerini çalıştırır; bu projede sonradan eklenen imzasız/manual issue'lar READY
olsa da çalıştırılmaz. Bu sürümde imza kontrolünü atlayan bir seçenek yoktur.
`--start-timer` henüz desteklenmez; mevcut timer adları projeler arasında
ortaktır. Birden fazla projede aynı timer'ı kurmayın. Ayrıntılar: [USAGE.md](USAGE.md).

## 1. Paketi aç

```bash
unzip ai-team-harness-v1.1.0.zip
cd ai-team-harness-v1.1.0
```

## 2. Hedef repoya kur

```bash
./install.sh /home/heisenberg/Desktop/Projects/vensift
```

Installer şunları sorar:

```text
Legacy planner/provider metadata [claude]:
Enable TypeSafe Jev decision engine? [y/N]:
```

Jev'i seçersen API key'i de güvenli/gizli girişle sorar. Key yalnızca
`HARNESS_SECRETS_FILE` ile gösterilen repo dışı secret store'a yazılır (dizin
0700, dosya 0600). `runtime.env` secret içermez.

Jev istemiyorsan `N` de. Harness normal rules routing ile çalışır.

## 3. Runtime ayarlarını tamamla

```bash
cd /home/heisenberg/Desktop/Projects/vensift
nano .ai-team/runtime/runtime.env
```

Örnek:

```bash
HARNESS_PROJECT_OWNER="Vensift"
HARNESS_PROJECT_NUMBER="1"
HARNESS_REPO="Vensift/website"
HARNESS_COORDINATOR_PROVIDER="claude"
HARNESS_DECISION_ENGINE="jev"
```

Jev kullanıyorsan `TYPESAFE_MODEL` runtime.env içinde kalır; API key'i görmek veya
yedeklemek için runtime.env içindeki `HARNESS_SECRETS_FILE` yolunu kullan:

```bash
TYPESAFE_MODEL="jev-latest"
```

## 4. Kontrol

```bash
.ai-team/bin/harness-doctor
```

Jev canlı test:

```bash
.ai-team/bin/harness-doctor --live
```

## 5. GitHub Project field'ları

```bash
.ai-team/bin/setup-github-project
```

## 6. Tek coordinator cycle

```bash
.ai-team/bin/coordinator-cycle
```

## 7. Non-stop

```bash
.ai-team/bin/install-systemd
systemctl --user enable --now ai-harness-coordinator.timer
```

Logout sonrasında da çalışmasını istiyorsan:

```bash
loginctl enable-linger "$USER"
```

## Jev'i sonradan kapatmak

```bash
HARNESS_DECISION_ENGINE="rules"
```

Jev'in kapatılması task/project state'ini değiştirmez.

## Jev kararını elle görmek

```bash
.ai-team/bin/decide task \
  --state-file .ai-team/examples/decision-task.json
```

Retry:

```bash
.ai-team/bin/decide retry \
  --state-file .ai-team/examples/decision-retry.json
```

Action routing:

```bash
.ai-team/bin/decide action \
  --state-file .ai-team/examples/decision-action.json
```
