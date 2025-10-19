package s3_manager

import (
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Manager struct {
	client       *s3.Client
	cfg          *Config
	storagePaths map[CatalogType]string // Соответствие типов каталогов паттернам путей в бакете. Используется для формирования пути к файлу в бакете. Например, "users" -> "users/%d/", "product_certificates" -> "products/%d/certificates/".
}

type Config struct {
	Endpoint               string
	Region                 string
	AccessKey              string
	SecretKey              string
	Name                   string        // Имя бакета
	RootCatalog            string        // Путь до нужного (корневого для сервиса) каталога в бакете. Например, "/examplesiteservice" для файлов определённого сервиса.
	CDN                    string        // CDN-ссылка для файлов в бакете (например, "https://cdn.examplesite.com"). Если заполнено, то заменяет собой хост ссылки при получении URL файлов.
	PresignedURLExpireTime time.Duration // Время жизни подписанной ссылки по умолчанию (например, 15 минут)
}

// Типы каталогов для хранения файлов в бакете. Используются для формирования пути к файлу в бакете.
// При создании нового каталога стоит придерживаться следующего правила чтобы избежать конфликтов имён при записи и сохранить единообразие структуры каталогов:
// Файлы, ссылки на которые хранятся во внутренних полях таблицы стоит класть в корень папки объекта.
// Если же ссылки на файлы хранятся в отдельной таблице (например, product_certificates для хранения сертификатов продукта)
// или состоят из множества файлов (например, видео формата .m3u8 состоит из множества файлов, но в базу записывается только ссылка на один мастер-файл),
// то стоит хранить их в соответствующей подпапке.
type CatalogType string

const PathCustomCatalog CatalogType = "custom_catalog" // Используется для загрузки файлов в каталог, указанный пользователем через StoragePath.CustomPath

// Информация о пути к файлу в бакете (для единичных файлов). Используется для формирования пути к файлу в бакете.
type StoragePath struct {
	RootCatalog string      // Путь к каталогу сервиса, если файлы сервиса хранятся не в корне бакета (например, "static/myproject/"). Используется для формирования полного пути к файлу в бакете.
	CatalogType CatalogType // Тип пути по назначению файла (например, "custom_catalog", "product", "product_certificates", "user" и т.д.). Если файл должен находиться в корне, то CatalogType должен быть пустым.
	EntityID    int64       // Идентификатор сущности (например, ID товара или пользователя). Используется, если CatalogType != "custom_catalog".
	CustomPath  string      // Кастомный путь к файлу в бакете (например, "custom/path/to/file/"). Используется, если CatalogType == "custom_catalog".
}

type BucketFile struct {
	File io.ReadSeeker
	Name string // Имя файла, включая расширение (например, "image.jpg")
}

// Информация о пути в бакете и списке файлов. Используется для загрузки нескольких файлов в бакет по одному пути.
type BucketFilesData struct {
	Path  StoragePath  // Путь к файлам в бакете
	Files []BucketFile // Список файлов для загрузки
}
