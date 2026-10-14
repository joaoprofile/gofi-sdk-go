CREATE TABLE products (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT TRUE
);

INSERT INTO products (name, description) VALUES
    ('Cafeteira Elétrica', 'Máquina de café expresso com vaporizador de leite'),
    ('Café em Grãos Especial', 'Café arábica torrado, notas de chocolate e caramelo'),
    ('Moedor de Café Manual', 'Moedor com mós de cerâmica para café coado'),
    ('Notebook Gamer', 'Placa de vídeo dedicada e tela de 144 Hz'),
    ('Notebook Ultrafino', 'Bateria de longa duração, ideal para trabalho'),
    ('Mouse sem Fio', 'Mouse ergonômico para notebook e desktop'),
    ('Teclado Mecânico', 'Switches táteis, iluminação RGB'),
    ('Chaleira Elétrica', 'Controle de temperatura para chá e café coado'),
    ('Garrafa Térmica', 'Mantém café e chá quentes por 12 horas');
