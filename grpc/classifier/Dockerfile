FROM python:3.11-slim

WORKDIR /app

RUN pip install --no-cache-dir grpcio protobuf

COPY infer_worker.py infer_pb2.py infer_pb2_grpc.py ./

EXPOSE 50051

CMD ["python", "infer_worker.py"]
