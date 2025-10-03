FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .

RUN pip install --no-cache-dir -r requirements.txt

COPY grpc_dispatcher.py infer_pb2.py infer_pb2_grpc.py .

EXPOSE 50052

CMD ["python", "grpc_dispatcher.py"]
