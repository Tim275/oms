package broker

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

func Connect(user, pass, host, port string) (*amqp.Channel, func())
 address := fmt.Sprintf("amp://%s:%s@%s:%s", user,pass,host,port)

conn,err := amqp.Dial(address)
if err != nil{

	log.Fatal(err)

}

err := ch.ExchangeDeclare( OrderCreatedEvent, "direct",true, false, false, false,nil)
if err != nil{

	log.Fatal(err)
}


err = ch.ExchangeDeclare(OrderCreatedPaid, true,false,false,false,nil)
if err != nil{

	log.Fatal(err)
}



return ch, connection.close