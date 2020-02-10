resource "aws_dynamodb_table" "book-building-notify-lambda-table" {
    name = "book-building-notify-lambda-table"
    read_capacity = 1
    write_capacity = 1
    hash_key = "code"
    attribute {
        name = "code"
        type = "S"
    }
}
